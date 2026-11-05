package settle

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/solanarpc"
)

const (
	// TokenProgram is the classic SPL Token program. Marketplace assets
	// (USDC) use it; Token-2022 mints are not supported.
	TokenProgram = chains.SPLTokenProgram
	// ATAProgram is the Associated Token Account program.
	ATAProgram = chains.AssociatedTokenProgram
	// MemoProgram is the SPL Memo program (v2).
	MemoProgram = "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr"

	baseFeeLamports  = 5000 // one signature
	tokenAccountSize = 165
	nativeKey        = "native"
)

// SolanaRPC is the subset of solanarpc.Client the adapter uses.
type SolanaRPC interface {
	Transaction(ctx context.Context, signature string) (*solanarpc.Transaction, error)
	ConfirmationStatus(ctx context.Context, signature string) (string, error)
	AccountInfo(ctx context.Context, addr string) (*solanarpc.AccountInfo, error)
	Balance(ctx context.Context, addr string) (uint64, error)
	TokenAccountsByOwner(ctx context.Context, owner, mint string) (uint64, error)
	MinimumBalanceForRentExemption(ctx context.Context, size int) (uint64, error)
}

type solana struct {
	clients map[string]SolanaRPC
}

func NewSolana(clients map[string]SolanaRPC) Adapter { return &solana{clients: clients} }

func (s *solana) client(chain chains.Chain) (SolanaRPC, error) {
	c, ok := s.clients[chain.Key]
	if !ok || c == nil {
		return nil, fmt.Errorf("settle: no solana rpc client for chain %q", chain.Key)
	}
	return c, nil
}

type atas struct{ buyer, seller, treasury string }

func (o Order) atas() (atas, error) {
	var a atas
	var err error
	if a.buyer, err = chains.AssociatedTokenAddress(o.Buyer, o.Mint, TokenProgram); err != nil {
		return a, fmt.Errorf("settle: buyer ata: %w", err)
	}
	if a.seller, err = chains.AssociatedTokenAddress(o.Seller, o.Mint, TokenProgram); err != nil {
		return a, fmt.Errorf("settle: seller ata: %w", err)
	}
	if a.treasury, err = chains.AssociatedTokenAddress(o.Treasury, o.Mint, TokenProgram); err != nil {
		return a, fmt.Errorf("settle: treasury ata: %w", err)
	}
	if a.seller == a.treasury {
		return a, fmt.Errorf("settle: seller and treasury resolve to the same token account")
	}
	return a, nil
}

func (s *solana) Quote(ctx context.Context, o Order) (Quote, error) {
	c, err := s.client(o.Chain)
	if err != nil {
		return Quote{}, err
	}
	a, err := o.atas()
	if err != nil {
		return Quote{}, err
	}
	rent, err := c.MinimumBalanceForRentExemption(ctx, tokenAccountSize)
	if err != nil {
		return Quote{}, err
	}
	q := Quote{RentPerATA: new(big.Int).SetUint64(rent)}
	si, err := c.AccountInfo(ctx, a.seller)
	if err != nil {
		return Quote{}, err
	}
	ti, err := c.AccountInfo(ctx, a.treasury)
	if err != nil {
		return Quote{}, err
	}
	q.CreateSellerATA, q.CreateTreasuryATA = si == nil, ti == nil
	fee := big.NewInt(baseFeeLamports)
	if q.CreateSellerATA {
		fee.Add(fee, q.RentPerATA)
	}
	if q.CreateTreasuryATA {
		fee.Add(fee, q.RentPerATA)
	}
	q.NetworkFee = fee
	return q, nil
}

func (s *solana) CheckFunds(ctx context.Context, o Order, q Quote) error {
	c, err := s.client(o.Chain)
	if err != nil {
		return err
	}
	if q.NetworkFee == nil {
		return fmt.Errorf("settle: quote has no network fee")
	}
	haveAsset, err := c.TokenAccountsByOwner(ctx, o.Buyer, o.Mint)
	if err != nil {
		return err
	}
	haveNative, err := c.Balance(ctx, o.Buyer)
	if err != nil {
		return err
	}
	ha, hn := new(big.Int).SetUint64(haveAsset), new(big.Int).SetUint64(haveNative)
	if ha.Cmp(o.Price) < 0 || hn.Cmp(q.NetworkFee) < 0 {
		return &FundsShortfall{
			NeedAsset: new(big.Int).Set(o.Price), HaveAsset: ha,
			NeedNative: new(big.Int).Set(q.NetworkFee), HaveNative: hn,
		}
	}
	return nil
}

func (s *solana) BuildTx(o Order, q Quote) (Instructions, error) {
	a, err := o.atas()
	if err != nil {
		return Instructions{}, err
	}
	create := func(owner, ata string) SolanaIx {
		return SolanaIx{Kind: "create_ata_idempotent", Accounts: map[string]string{
			"payer": o.Buyer, "ata": ata, "owner": owner, "mint": o.Mint,
			"systemProgram": chains.SystemProgram, "tokenProgram": TokenProgram,
		}}
	}
	transfer := func(dest string, amt *big.Int) SolanaIx {
		return SolanaIx{Kind: "transfer_checked", Accounts: map[string]string{
			"source": a.buyer, "destination": dest, "mint": o.Mint, "authority": o.Buyer,
		}, Amount: amt.String(), Decimals: o.Decimals}
	}
	var ixs []SolanaIx
	if q.CreateTreasuryATA {
		ixs = append(ixs, create(o.Treasury, a.treasury))
	}
	if q.CreateSellerATA {
		ixs = append(ixs, create(o.Seller, a.seller))
	}
	ixs = append(ixs, transfer(a.treasury, o.Fee), transfer(a.seller, o.SellerAmount),
		SolanaIx{Kind: "memo", Memo: memoText(o)})
	return Instructions{Solana: ixs}, nil
}

func memoText(o Order) string { return "max:sale:" + o.SaleID.String() }

// Distinct mismatch reasons.
const (
	reasonWrongSender   = "fee payer is not the buyer"
	reasonNoTreasury    = "no transferChecked of the mint to the treasury account"
	reasonManyTreasury  = "more than one transferChecked of the mint to the treasury account"
	reasonWrongFee      = "treasury transfer amount does not equal the fee"
	reasonNoSeller      = "no transferChecked of the mint to the seller account"
	reasonManySeller    = "more than one transferChecked of the mint to the seller account"
	reasonWrongSplit    = "seller transfer amount does not equal the seller amount"
	reasonWrongAuth     = "transfer authority is not the buyer"
	reasonExtraTransfer = "buyer authorizes another transfer of the mint"
	reasonNoMemo        = "sale memo is missing"
)

func (s *solana) Observe(ctx context.Context, o Order, txHash string) (Observation, error) {
	c, err := s.client(o.Chain)
	if err != nil {
		return Observation{}, err
	}
	a, err := o.atas()
	if err != nil {
		return Observation{}, err
	}
	tx, err := c.Transaction(ctx, txHash)
	if err != nil {
		return Observation{}, err
	}
	if tx == nil {
		return Observation{Status: StatusNotFound, Reason: "transaction not found"}, nil
	}
	if tx.Failed() {
		return Observation{Status: StatusFailed, Reason: "transaction failed on chain"}, nil
	}
	status, err := c.ConfirmationStatus(ctx, txHash)
	if err != nil {
		return Observation{}, err
	}
	if status != "finalized" {
		return Observation{Status: StatusPending, Reason: "transaction not finalized"}, nil
	}
	obs := Observation{Status: StatusConfirmed, Confirmations: 1}
	if obs.Reason = matchSale(tx, o, a); obs.Reason == "" {
		obs.Matches = true
	}
	return obs, nil
}

type parsedIx struct {
	Type string `json:"type"`
	Info struct {
		Source            string `json:"source"`
		Destination       string `json:"destination"`
		Authority         string `json:"authority"`
		MultisigAuthority string `json:"multisigAuthority"`
		Mint              string `json:"mint"`
		Amount            string `json:"amount"`
		TokenAmount       struct {
			Amount string `json:"amount"`
		} `json:"tokenAmount"`
	} `json:"info"`
}

func amountIs(s string, want *big.Int) bool {
	v, ok := new(big.Int).SetString(s, 10)
	return ok && want != nil && v.Cmp(want) == 0
}

// matchSale returns "" only when the transaction is exactly the sale.
func matchSale(tx *solanarpc.Transaction, o Order, a atas) string {
	if tx.FeePayer() != o.Buyer {
		return reasonWrongSender
	}
	wantMemo := memoText(o)
	var memo bool
	var toTreasury, toSeller []parsedIx
	wrongAuth, extra := false, false

	for _, ins := range tx.AllInstructions() {
		if ins.ProgramID == MemoProgram {
			var text string
			if json.Unmarshal(ins.Parsed, &text) == nil && text == wantMemo {
				memo = true
			}
			continue
		}
		if ins.ProgramID != TokenProgram || len(ins.Parsed) == 0 || ins.Parsed[0] != '{' {
			continue
		}
		var p parsedIx
		if json.Unmarshal(ins.Parsed, &p) != nil {
			continue
		}
		byBuyer := p.Info.Authority == o.Buyer || p.Info.MultisigAuthority == o.Buyer
		switch p.Type {
		case "transferChecked":
			if p.Info.Mint != o.Mint {
				continue
			}
			switch p.Info.Destination {
			case a.treasury:
				toTreasury = append(toTreasury, p)
			case a.seller:
				toSeller = append(toSeller, p)
			default:
				if byBuyer {
					extra = true
				}
			}
		case "transfer":
			// Unchecked transfers carry no mint; any from the buyer's
			// token account for this mint is an extra spend.
			if byBuyer || p.Info.Source == a.buyer {
				extra = true
			}
		}
	}
	for _, p := range append(append([]parsedIx{}, toTreasury...), toSeller...) {
		if p.Info.Authority != o.Buyer {
			wrongAuth = true
		}
	}

	switch {
	case len(toTreasury) == 0:
		return reasonNoTreasury
	case len(toTreasury) > 1:
		return reasonManyTreasury
	case len(toSeller) == 0:
		return reasonNoSeller
	case len(toSeller) > 1:
		return reasonManySeller
	case !amountIs(toTreasury[0].Info.TokenAmount.Amount, o.Fee):
		return reasonWrongFee
	case !amountIs(toSeller[0].Info.TokenAmount.Amount, o.SellerAmount):
		return reasonWrongSplit
	case wrongAuth:
		return reasonWrongAuth
	case extra:
		return reasonExtraTransfer
	case !memo:
		return reasonNoMemo
	}
	return ""
}

func (s *solana) Balances(ctx context.Context, chain chains.Chain, owner string) (map[string]*big.Int, error) {
	c, err := s.client(chain)
	if err != nil {
		return nil, err
	}
	native, err := c.Balance(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := map[string]*big.Int{nativeKey: new(big.Int).SetUint64(native)}
	for _, t := range chains.VerifiedTokensFor(chain.Key) {
		v, err := c.TokenAccountsByOwner(ctx, owner, t.Address)
		if err != nil {
			return nil, err
		}
		out[t.Address] = new(big.Int).SetUint64(v)
	}
	return out, nil
}
