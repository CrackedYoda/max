package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/evmrpc"
	"github.com/max/max-core/internal/solanarpc"
)

// Clock skew allowed between a payment's creation and the block that includes it.
const blockTimeSkew = 2 * time.Minute

type EVMClient interface {
	ChainID(ctx context.Context) (int64, error)
	BlockNumber(ctx context.Context) (int64, error)
	Receipt(ctx context.Context, hash string) (*evmrpc.Receipt, error)
	Transaction(ctx context.Context, hash string) (*evmrpc.Transaction, error)
	BlockTimestamp(ctx context.Context, blockNumberHex string) (int64, error)
	HasCode(ctx context.Context, addr string) (bool, error)
	ERC20Decimals(ctx context.Context, token string) (int, error)
}

type SolanaClient interface {
	Transaction(ctx context.Context, signature string) (*solanarpc.Transaction, error)
	ConfirmationStatus(ctx context.Context, signature string) (string, error)
	AccountInfo(ctx context.Context, addr string) (*solanarpc.AccountInfo, error)
}

type gateway struct {
	evm    map[string]EVMClient
	solana map[string]SolanaClient

	mu          sync.Mutex
	verifiedIDs map[string]bool
}

var _ Gateway = (*gateway)(nil)

// NewGateway builds a gateway from chain key → RPC URL. Chains without a URL
// are reported as not configured and payments on them are refused.
func NewGateway(rpcURLs map[string]string) Gateway {
	evm := map[string]EVMClient{}
	sol := map[string]SolanaClient{}
	for key, url := range rpcURLs {
		c, ok := chains.Lookup(key)
		if !ok || url == "" {
			continue
		}
		switch c.Namespace {
		case chains.NamespaceEVM:
			evm[c.Key] = evmrpc.New(url)
		case chains.NamespaceSolana:
			sol[c.Key] = solanarpc.New(url)
		}
	}
	return newGateway(evm, sol)
}

func newGateway(evm map[string]EVMClient, sol map[string]SolanaClient) *gateway {
	return &gateway{evm: evm, solana: sol, verifiedIDs: map[string]bool{}}
}

func (g *gateway) Configured(chainKey string) bool {
	_, e := g.evm[chainKey]
	_, s := g.solana[chainKey]
	return e || s
}

// evmClient returns the client for a chain after confirming once that its RPC
// really serves that chain, so a misconfigured URL can't verify payments
// against the wrong network.
func (g *gateway) evmClient(ctx context.Context, chain chains.Chain) (EVMClient, error) {
	c, ok := g.evm[chain.Key]
	if !ok {
		return nil, ErrChainNotConfigured
	}
	g.mu.Lock()
	verified := g.verifiedIDs[chain.Key]
	g.mu.Unlock()
	if verified {
		return c, nil
	}
	id, err := c.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	if id != chain.EVMChainID {
		return nil, fmt.Errorf("RPC for %s reports chain id %d, expected %d", chain.Key, id, chain.EVMChainID)
	}
	g.mu.Lock()
	g.verifiedIDs[chain.Key] = true
	g.mu.Unlock()
	return c, nil
}

func (g *gateway) solanaClient(chain chains.Chain) (SolanaClient, error) {
	c, ok := g.solana[chain.Key]
	if !ok {
		return nil, ErrChainNotConfigured
	}
	return c, nil
}

func (g *gateway) TokenInfo(ctx context.Context, chain chains.Chain, token string) (TokenInfo, error) {
	switch chain.Namespace {
	case chains.NamespaceEVM:
		c, err := g.evmClient(ctx, chain)
		if err != nil {
			return TokenInfo{}, err
		}
		hasCode, err := c.HasCode(ctx, token)
		if err != nil {
			return TokenInfo{}, err
		}
		if !hasCode {
			return TokenInfo{}, errors.New("no contract deployed at token address")
		}
		d, err := c.ERC20Decimals(ctx, token)
		if err != nil {
			return TokenInfo{}, err
		}
		return TokenInfo{Decimals: d}, nil

	case chains.NamespaceSolana:
		c, err := g.solanaClient(chain)
		if err != nil {
			return TokenInfo{}, err
		}
		acct, err := c.AccountInfo(ctx, token)
		if err != nil {
			return TokenInfo{}, err
		}
		if acct == nil || acct.Data.Parsed == nil || acct.Data.Parsed.Type != "mint" || !chains.IsSPLTokenProgram(acct.Owner) {
			return TokenInfo{}, errors.New("address is not an SPL token mint")
		}
		var info struct {
			Decimals int `json:"decimals"`
		}
		if err := json.Unmarshal(acct.Data.Parsed.Info, &info); err != nil {
			return TokenInfo{}, err
		}
		return TokenInfo{Decimals: info.Decimals, TokenProgram: acct.Owner}, nil
	}
	return TokenInfo{}, chains.ErrUnsupportedNamespace
}

func (g *gateway) AccountExists(ctx context.Context, chain chains.Chain, addr string) (bool, error) {
	c, err := g.solanaClient(chain)
	if err != nil {
		return false, err
	}
	acct, err := c.AccountInfo(ctx, addr)
	return acct != nil, err
}

func (g *gateway) Observe(ctx context.Context, p Payment) (Observation, error) {
	switch p.Chain.Namespace {
	case chains.NamespaceEVM:
		return g.observeEVM(ctx, p)
	case chains.NamespaceSolana:
		return g.observeSolana(ctx, p)
	}
	return Observation{}, chains.ErrUnsupportedNamespace
}

func (g *gateway) observeEVM(ctx context.Context, p Payment) (Observation, error) {
	c, err := g.evmClient(ctx, p.Chain)
	if err != nil {
		return Observation{}, err
	}
	rcpt, err := c.Receipt(ctx, p.TxHash)
	if err != nil {
		return Observation{}, err
	}
	if rcpt == nil || rcpt.BlockNumber == "" {
		return Observation{Found: false}, nil
	}
	obs := Observation{Found: true}
	if rcpt.Status != "0x1" {
		obs.Reverted = true
		return obs, nil
	}

	ts, err := c.BlockTimestamp(ctx, rcpt.BlockNumber)
	if err != nil {
		return Observation{}, err
	}
	if time.Unix(ts, 0).Before(p.CreatedAt.Add(-blockTimeSkew)) {
		obs.Mismatch = "transaction was mined before this payment intent was created"
		return obs, nil
	}

	if p.Asset.Kind == AssetNative {
		tx, err := c.Transaction(ctx, p.TxHash)
		if err != nil {
			return Observation{}, err
		}
		if tx == nil {
			return Observation{Found: false}, nil
		}
		obs.Mismatch = matchEVMNative(tx, p)
	} else {
		obs.Mismatch = matchERC20Transfer(rcpt.Logs, p)
	}
	if obs.Mismatch != "" {
		return obs, nil
	}

	mined, ok := chains.ParseHexBig(rcpt.BlockNumber)
	if !ok {
		return Observation{}, fmt.Errorf("invalid block number %q", rcpt.BlockNumber)
	}
	head, err := c.BlockNumber(ctx)
	if err != nil {
		return Observation{}, err
	}
	obs.Confirmations = head - mined.Int64() + 1
	if obs.Confirmations < 0 {
		obs.Confirmations = 0
	}
	obs.Final = obs.Confirmations >= p.RequiredConfirmations
	return obs, nil
}

func matchEVMNative(tx *evmrpc.Transaction, p Payment) string {
	if !strings.EqualFold(tx.From, p.SenderAddress) {
		return "sent from a different address than the payment's sender"
	}
	if !strings.EqualFold(tx.To, p.PayTo()) {
		return "sent to a different address than the resolved recipient"
	}
	v, ok := chains.ParseHexBig(tx.Value)
	if !ok || v.Cmp(p.Amount) != 0 {
		return "transferred value differs from the payment amount"
	}
	return ""
}

// matchERC20Transfer looks for the exact Transfer(sender → recipient, amount)
// event from the expected token contract, which holds no matter how the
// transfer was routed (direct call, smart wallet, multicall).
func matchERC20Transfer(logs []evmrpc.Log, p Payment) string {
	from, to := chains.AddressTopic(p.SenderAddress), chains.AddressTopic(p.PayTo())
	for _, l := range logs {
		if l.Removed || !strings.EqualFold(l.Address, p.Asset.Address) || len(l.Topics) != 3 {
			continue
		}
		if !strings.EqualFold(l.Topics[0], chains.ERC20TransferTopic) ||
			!strings.EqualFold(l.Topics[1], from) || !strings.EqualFold(l.Topics[2], to) {
			continue
		}
		if v, ok := chains.ParseHexBig(l.Data); ok && v.Cmp(p.Amount) == 0 {
			return ""
		}
	}
	return "no matching token Transfer from sender to recipient for the exact amount"
}

func (g *gateway) observeSolana(ctx context.Context, p Payment) (Observation, error) {
	c, err := g.solanaClient(p.Chain)
	if err != nil {
		return Observation{}, err
	}
	tx, err := c.Transaction(ctx, p.TxHash)
	if err != nil {
		return Observation{}, err
	}
	if tx == nil {
		return Observation{Found: false}, nil
	}
	obs := Observation{Found: true}
	if tx.Failed() {
		obs.Reverted = true
		return obs, nil
	}
	if tx.BlockTime != nil && time.Unix(*tx.BlockTime, 0).Before(p.CreatedAt.Add(-blockTimeSkew)) {
		obs.Mismatch = "transaction landed before this payment intent was created"
		return obs, nil
	}
	if obs.Mismatch = matchSolanaTransfer(tx, p); obs.Mismatch != "" {
		return obs, nil
	}

	status, err := c.ConfirmationStatus(ctx, p.TxHash)
	if err != nil {
		return Observation{}, err
	}
	if status == "finalized" {
		obs.Confirmations, obs.Final = 1, true
	}
	return obs, nil
}

type solanaParsed struct {
	Type string `json:"type"`
	Info struct {
		Source            string      `json:"source"`
		Destination       string      `json:"destination"`
		Lamports          json.Number `json:"lamports"`
		Authority         string      `json:"authority"`
		MultisigAuthority string      `json:"multisigAuthority"`
		Mint              string      `json:"mint"`
		Amount            string      `json:"amount"`
		TokenAmount       struct {
			Amount string `json:"amount"`
		} `json:"tokenAmount"`
	} `json:"info"`
}

func matchSolanaTransfer(tx *solanarpc.Transaction, p Payment) string {
	var destATA string
	if p.Asset.Kind == AssetToken {
		destATA, _ = chains.AssociatedTokenAddress(p.PayTo(), p.Asset.Address, p.Asset.TokenProgram)
	}

	for _, ins := range tx.AllInstructions() {
		if len(ins.Parsed) == 0 || ins.Parsed[0] != '{' {
			continue
		}
		var parsed solanaParsed
		dec := json.NewDecoder(strings.NewReader(string(ins.Parsed)))
		dec.UseNumber()
		if err := dec.Decode(&parsed); err != nil {
			continue
		}

		if p.Asset.Kind == AssetNative {
			if ins.ProgramID != chains.SystemProgram || parsed.Type != "transfer" {
				continue
			}
			if parsed.Info.Source == p.SenderAddress && parsed.Info.Destination == p.PayTo() &&
				bigEquals(parsed.Info.Lamports.String(), p.Amount) {
				return ""
			}
			continue
		}

		if ins.ProgramID != p.Asset.TokenProgram {
			continue
		}
		authority := parsed.Info.Authority
		if authority == "" {
			authority = parsed.Info.MultisigAuthority
		}
		if parsed.Info.Destination != destATA || authority != p.SenderAddress {
			continue
		}
		switch parsed.Type {
		case "transferChecked":
			if parsed.Info.Mint == p.Asset.Address && bigEquals(parsed.Info.TokenAmount.Amount, p.Amount) {
				return ""
			}
		case "transfer":
			// Unchecked transfers carry no mint, but the destination ATA is
			// derived from the mint, so matching it pins the token.
			if bigEquals(parsed.Info.Amount, p.Amount) {
				return ""
			}
		}
	}
	if p.Asset.Kind == AssetNative {
		return "no SOL transfer from sender to recipient for the exact amount"
	}
	return "no token transfer from sender to the recipient's token account for the exact amount"
}

func bigEquals(s string, want *big.Int) bool {
	v, ok := new(big.Int).SetString(s, 10)
	return ok && v.Cmp(want) == 0
}
