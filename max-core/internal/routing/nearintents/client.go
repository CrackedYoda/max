// Package nearintents implements routing.Provider on the NEAR Intents 1Click API.
// Spec: https://1click.chaindefuser.com/docs/v0/openapi.yaml
package nearintents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/routing"
)

const (
	DefaultBaseURL = "https://1click.chaindefuser.com"
	ProviderName   = "near_intents"
	tokensTTL      = 5 * time.Minute
)

// blockchainCodes maps MAX chain keys to 1Click blockchain identifiers.
// 1Click is mainnet-only, so testnets are deliberately absent.
var blockchainCodes = map[string]string{
	"ethereum": "eth",
	"base":     "base",
	"arbitrum": "arb",
	"optimism": "op",
	"polygon":  "pol",
	"solana":   "sol",
}

type Config struct {
	BaseURL string
	// APIKey is optional; partner keys from partners.near-intents.org waive the
	// platform fee charged on unauthenticated quotes.
	APIKey   string
	Referral string
}

type Client struct {
	cfg  Config
	http *http.Client

	mu        sync.Mutex
	tokens    []token
	tokensExp time.Time
}

var _ routing.Provider = (*Client)(nil)

func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) Name() string { return ProviderName }

type token struct {
	AssetID         string  `json:"assetId"`
	Decimals        int     `json:"decimals"`
	Blockchain      string  `json:"blockchain"`
	Symbol          string  `json:"symbol"`
	Price           float64 `json:"price"`
	ContractAddress string  `json:"contractAddress"`
}

func (c *Client) listTokens(ctx context.Context) ([]token, error) {
	c.mu.Lock()
	if time.Now().Before(c.tokensExp) {
		t := c.tokens
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()

	var out []token
	if err := c.do(ctx, http.MethodGet, "/v0/tokens", nil, &out); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.tokens, c.tokensExp = out, time.Now().Add(tokensTTL)
	c.mu.Unlock()
	return out, nil
}

func (c *Client) Asset(ctx context.Context, ref routing.AssetRef) (routing.ProviderAsset, error) {
	code, ok := blockchainCodes[ref.Chain.Key]
	if !ok {
		return routing.ProviderAsset{}, routing.ErrRouteUnavailable
	}
	tokens, err := c.listTokens(ctx)
	if err != nil {
		return routing.ProviderAsset{}, err
	}

	var match *token
	for i := range tokens {
		t := &tokens[i]
		if t.Blockchain != code {
			continue
		}
		if ref.Kind == routing.AssetNative {
			if t.ContractAddress == "" && (match == nil || t.Symbol == ref.Chain.NativeCoin) {
				match = t
			}
			continue
		}
		if sameAddress(ref.Chain.Namespace, t.ContractAddress, ref.Address) {
			match = t
			break
		}
	}
	if match == nil {
		return routing.ProviderAsset{}, routing.ErrRouteUnavailable
	}
	return routing.ProviderAsset{ID: match.AssetID, Decimals: match.Decimals, Symbol: match.Symbol}, nil
}

type quoteRequest struct {
	Dry                bool   `json:"dry"`
	DepositMode        string `json:"depositMode"`
	SwapType           string `json:"swapType"`
	SlippageTolerance  int    `json:"slippageTolerance"`
	OriginAsset        string `json:"originAsset"`
	DepositType        string `json:"depositType"`
	DestinationAsset   string `json:"destinationAsset"`
	Amount             string `json:"amount"`
	RefundTo           string `json:"refundTo"`
	RefundType         string `json:"refundType"`
	Recipient          string `json:"recipient"`
	RecipientType      string `json:"recipientType"`
	Deadline           string `json:"deadline"`
	Referral           string `json:"referral,omitempty"`
	QuoteWaitingTimeMs int    `json:"quoteWaitingTimeMs"`
}

type quoteResponse struct {
	CorrelationID string       `json:"correlationId"`
	Timestamp     string       `json:"timestamp"`
	Signature     string       `json:"signature"`
	QuoteRequest  quoteRequest `json:"quoteRequest"`
	Quote         struct {
		DepositAddress string  `json:"depositAddress"`
		DepositMemo    string  `json:"depositMemo"`
		AmountIn       string  `json:"amountIn"`
		AmountInUsd    string  `json:"amountInUsd"`
		MinAmountIn    string  `json:"minAmountIn"`
		AmountOut      string  `json:"amountOut"`
		AmountOutUsd   string  `json:"amountOutUsd"`
		MinAmountOut   string  `json:"minAmountOut"`
		Deadline       string  `json:"deadline"`
		TimeEstimate   float64 `json:"timeEstimate"`
	} `json:"quote"`
}

func (c *Client) Quote(ctx context.Context, req routing.QuoteRequest) (routing.Quote, error) {
	origin, err := c.Asset(ctx, req.Origin)
	if err != nil {
		return routing.Quote{}, err
	}
	dest, err := c.Asset(ctx, req.Destination)
	if err != nil {
		return routing.Quote{}, err
	}

	body := quoteRequest{
		Dry: false, DepositMode: "SIMPLE", SwapType: string(req.SwapType),
		SlippageTolerance: req.SlippageBps, OriginAsset: origin.ID, DepositType: "ORIGIN_CHAIN",
		DestinationAsset: dest.ID, Amount: req.Amount.String(),
		RefundTo: req.RefundTo, RefundType: "ORIGIN_CHAIN",
		Recipient: req.Recipient, RecipientType: "DESTINATION_CHAIN",
		Deadline: req.Deadline.UTC().Format(time.RFC3339), Referral: c.cfg.Referral,
		QuoteWaitingTimeMs: 3000,
	}

	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/v0/quote", body, &raw); err != nil {
		return routing.Quote{}, err
	}
	var resp quoteResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return routing.Quote{}, fmt.Errorf("decode quote: %w", err)
	}
	if err := checkEcho(body, resp.QuoteRequest, req.Origin.Chain.Namespace, req.Destination.Chain.Namespace); err != nil {
		return routing.Quote{}, err
	}
	if resp.Quote.DepositMemo != "" {
		// None of MAX's chains use memo deposits; a memo here means something is off.
		return routing.Quote{}, errors.New("quote requires a deposit memo, which MAX does not support")
	}

	q := routing.Quote{
		Provider: ProviderName, OriginAssetID: origin.ID, DestinationAssetID: dest.ID,
		DepositAddress: resp.Quote.DepositAddress,
		AmountInUSD:    resp.Quote.AmountInUsd, AmountOutUSD: resp.Quote.AmountOutUsd,
		TimeEstimate: time.Duration(resp.Quote.TimeEstimate * float64(time.Second)),
		Signature:    resp.Signature, CorrelationID: resp.CorrelationID, Raw: raw,
	}
	for dst, src := range map[**big.Int]string{
		&q.AmountIn: resp.Quote.AmountIn, &q.MinAmountIn: resp.Quote.MinAmountIn,
		&q.AmountOut: resp.Quote.AmountOut, &q.MinAmountOut: resp.Quote.MinAmountOut,
	} {
		if src == "" {
			continue
		}
		v, ok := new(big.Int).SetString(src, 10)
		if !ok {
			return routing.Quote{}, fmt.Errorf("quote has non-integer amount %q", src)
		}
		*dst = v
	}
	if resp.Quote.Deadline != "" {
		if q.Deadline, err = time.Parse(time.RFC3339, resp.Quote.Deadline); err != nil {
			return routing.Quote{}, fmt.Errorf("quote deadline: %w", err)
		}
	}
	return q, nil
}

// checkEcho ensures the provider quoted exactly what was requested. A
// mismatch in recipient or refund address would send funds to the wrong place.
func checkEcho(sent, got quoteRequest, originNS, destNS chains.Namespace) error {
	switch {
	case got.OriginAsset != sent.OriginAsset, got.DestinationAsset != sent.DestinationAsset:
		return errors.New("quote assets differ from request")
	case got.Amount != sent.Amount:
		return errors.New("quote amount differs from request")
	case got.SwapType != sent.SwapType:
		return errors.New("quote swap type differs from request")
	case !sameAddress(destNS, got.Recipient, sent.Recipient):
		return errors.New("quote recipient differs from request")
	case !sameAddress(originNS, got.RefundTo, sent.RefundTo):
		return errors.New("quote refund address differs from request")
	case got.RecipientType != "DESTINATION_CHAIN", got.RefundType != "ORIGIN_CHAIN", got.DepositType != "ORIGIN_CHAIN":
		return errors.New("quote routing types differ from request")
	}
	return nil
}

func (c *Client) SubmitDeposit(ctx context.Context, depositAddress, txHash string) error {
	body := map[string]string{"txHash": txHash, "depositAddress": depositAddress}
	return c.do(ctx, http.MethodPost, "/v0/deposit/submit", body, nil)
}

type statusResponse struct {
	Status      string `json:"status"`
	UpdatedAt   string `json:"updatedAt"`
	SwapDetails struct {
		AmountOut                string          `json:"amountOut"`
		RefundedAmount           string          `json:"refundedAmount"`
		RefundReason             string          `json:"refundReason"`
		OriginChainTxHashes      []routing.TxRef `json:"originChainTxHashes"`
		DestinationChainTxHashes []routing.TxRef `json:"destinationChainTxHashes"`
	} `json:"swapDetails"`
}

func (c *Client) Status(ctx context.Context, depositAddress string) (routing.ExecutionStatus, error) {
	var resp statusResponse
	path := "/v0/status?depositAddress=" + url.QueryEscape(depositAddress)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return routing.ExecutionStatus{}, err
	}
	out := routing.ExecutionStatus{
		State:          routing.ExecutionState(resp.Status),
		RefundReason:   resp.SwapDetails.RefundReason,
		OriginTxs:      resp.SwapDetails.OriginChainTxHashes,
		DestinationTxs: resp.SwapDetails.DestinationChainTxHashes,
	}
	if t, err := time.Parse(time.RFC3339, resp.UpdatedAt); err == nil {
		out.UpdatedAt = t
	}
	if v, ok := new(big.Int).SetString(resp.SwapDetails.AmountOut, 10); ok {
		out.AmountOut = v
	}
	if v, ok := new(big.Int).SetString(resp.SwapDetails.RefundedAmount, 10); ok {
		out.RefundedAmount = v
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("1click %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}

	switch {
	case resp.StatusCode == http.StatusNotFound && strings.HasPrefix(path, "/v0/status"):
		return routing.ErrDepositNotFound
	case resp.StatusCode == http.StatusBadRequest:
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Message == "" {
			e.Message = "provider rejected the request"
		}
		return &routing.QuoteRejectedError{Reason: e.Message}
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("1click rejected the API key")
	case resp.StatusCode >= 300:
		return fmt.Errorf("1click %s %s: http %d", method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("1click %s: decode: %w", path, err)
	}
	return nil
}

func sameAddress(ns chains.Namespace, a, b string) bool {
	if ns == chains.NamespaceEVM {
		return strings.EqualFold(a, b)
	}
	return a == b
}
