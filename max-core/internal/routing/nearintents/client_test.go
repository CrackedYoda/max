package nearintents

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/routing"
)

const tokensJSON = `[
 {"assetId":"nep141:base.omft.near","decimals":18,"blockchain":"base","symbol":"ETH","price":2500,"priceUpdatedAt":"2026-01-01T00:00:00Z"},
 {"assetId":"nep141:base-0x833589fcd6edb6e08f4c7c32d4f71b54bda02913.omft.near","decimals":6,"blockchain":"base","symbol":"USDC","price":1,"priceUpdatedAt":"2026-01-01T00:00:00Z","contractAddress":"0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"},
 {"assetId":"nep141:sol.omft.near","decimals":9,"blockchain":"sol","symbol":"SOL","price":150,"priceUpdatedAt":"2026-01-01T00:00:00Z"},
 {"assetId":"nep141:sol-5ce3bf3a31af18be40ba30f721101b4341690186.omft.near","decimals":6,"blockchain":"sol","symbol":"USDC","price":1,"priceUpdatedAt":"2026-01-01T00:00:00Z","contractAddress":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},
 {"assetId":"nep141:btc.omft.near","decimals":8,"blockchain":"btc","symbol":"BTC","price":100000,"priceUpdatedAt":"2026-01-01T00:00:00Z"},
 {"assetId":"nep141:ltc.omft.near","decimals":8,"blockchain":"ltc","symbol":"LTC","price":90,"priceUpdatedAt":"2026-01-01T00:00:00Z"},
 {"assetId":"nep141:tron-d28a265909efecdcee7c5028585214ea0b96f015.omft.near","decimals":6,"blockchain":"tron","symbol":"USDT","price":1,"priceUpdatedAt":"2026-01-01T00:00:00Z","contractAddress":"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"},
 {"assetId":"nep141:tron.omft.near","decimals":6,"blockchain":"tron","symbol":"TRX","price":0.3,"priceUpdatedAt":"2026-01-01T00:00:00Z"}
]`

const (
	evmSender = "0x1111111111111111111111111111111111111111"
	solRecip  = "4Nd1mBQtrMJVYVfKf2PJy9NZUZdTAsp7D4xWLs4gDB4T"
	solUSDC   = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
)

func chain(k string) chains.Chain { c, _ := chains.Lookup(k); return c }

type fake struct {
	t          *testing.T
	quoteHook  func(req map[string]any) (int, any)
	statusCode int
	statusBody string
	sawAPIKey  string
}

func (f *fake) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.sawAPIKey = r.Header.Get("X-API-Key")
		switch {
		case r.URL.Path == "/v0/tokens":
			w.Write([]byte(tokensJSON))
		case r.URL.Path == "/v0/quote":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			code, body := f.quoteHook(req)
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(body)
		case r.URL.Path == "/v0/status":
			if f.statusCode != 0 {
				w.WriteHeader(f.statusCode)
			}
			w.Write([]byte(f.statusBody))
		case r.URL.Path == "/v0/deposit/submit":
			w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	}))
}

func echoQuote(req map[string]any, mutate func(q, echo map[string]any)) (int, any) {
	q := map[string]any{
		"depositAddress": "0x9999999999999999999999999999999999999999",
		"amountIn":       req["amount"], "amountInFormatted": "1", "amountInUsd": "2500",
		"minAmountIn": req["amount"], "amountOut": "2490000000", "amountOutFormatted": "2490",
		"amountOutUsd": "2490", "minAmountOut": "2465100000", "deadline": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"timeEstimate": 45,
	}
	echo := map[string]any{}
	for k, v := range req {
		echo[k] = v
	}
	if mutate != nil {
		mutate(q, echo)
	}
	return 200, map[string]any{"correlationId": "c1", "timestamp": "2026-01-01T00:00:00Z", "signature": "ed25519:sig", "quoteRequest": echo, "quote": q}
}

func baseReq() routing.QuoteRequest {
	return routing.QuoteRequest{
		Origin:      routing.AssetRef{Chain: chain("base"), Kind: routing.AssetNative},
		Destination: routing.AssetRef{Chain: chain("solana"), Kind: routing.AssetToken, Address: solUSDC},
		SwapType:    routing.ExactInput, Amount: big.NewInt(1_000_000_000_000_000_000),
		Recipient: solRecip, RefundTo: chains.ChecksumEVMAddress(evmSender), SlippageBps: 100,
		Deadline: time.Now().Add(30 * time.Minute),
	}
}

func TestAsset(t *testing.T) {
	f := &fake{t: t}
	srv := f.server()
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL})
	ctx := context.Background()

	tests := []struct {
		name    string
		ref     routing.AssetRef
		wantID  string
		wantErr error
	}{
		{"native ETH on Base", routing.AssetRef{Chain: chain("base"), Kind: routing.AssetNative}, "nep141:base.omft.near", nil},
		{"USDC on Base by checksummed address", routing.AssetRef{Chain: chain("base"), Kind: routing.AssetToken, Address: chains.ChecksumEVMAddress("0x833589fcd6edb6e08f4c7c32d4f71b54bda02913")}, "nep141:base-0x833589fcd6edb6e08f4c7c32d4f71b54bda02913.omft.near", nil},
		{"USDC on Solana by mint", routing.AssetRef{Chain: chain("solana"), Kind: routing.AssetToken, Address: solUSDC}, "nep141:sol-5ce3bf3a31af18be40ba30f721101b4341690186.omft.near", nil},
		{"Solana mint is case-sensitive", routing.AssetRef{Chain: chain("solana"), Kind: routing.AssetToken, Address: strings.ToLower(solUSDC)}, "", routing.ErrRouteUnavailable},
		{"unlisted token", routing.AssetRef{Chain: chain("solana"), Kind: routing.AssetToken, Address: "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"}, "", routing.ErrRouteUnavailable},
		{"native BTC", routing.AssetRef{Chain: chain("bitcoin"), Kind: routing.AssetNative}, "nep141:btc.omft.near", nil},
		{"native LTC", routing.AssetRef{Chain: chain("litecoin"), Kind: routing.AssetNative}, "nep141:ltc.omft.near", nil},
		{"native TRX, not Tron USDT", routing.AssetRef{Chain: chain("tron"), Kind: routing.AssetNative}, "nep141:tron.omft.near", nil},
		{"testnets are not routable", routing.AssetRef{Chain: chain("base-sepolia"), Kind: routing.AssetNative}, "", routing.ErrRouteUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := c.Asset(ctx, tt.ref)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if a.ID != tt.wantID {
				t.Errorf("id = %s, want %s", a.ID, tt.wantID)
			}
		})
	}
}

func TestQuote(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		hook    func(req map[string]any) (int, any)
		wantErr string
	}{
		{"valid quote", func(r map[string]any) (int, any) { return echoQuote(r, nil) }, ""},
		{"provider swapped the recipient", func(r map[string]any) (int, any) {
			return echoQuote(r, func(_, e map[string]any) { e["recipient"] = "AttackerAddr1111111111111111111111111111111" })
		}, "recipient differs"},
		{"provider swapped the refund address", func(r map[string]any) (int, any) {
			return echoQuote(r, func(_, e map[string]any) { e["refundTo"] = "0x3333333333333333333333333333333333333333" })
		}, "refund address differs"},
		{"provider changed the amount", func(r map[string]any) (int, any) {
			return echoQuote(r, func(_, e map[string]any) { e["amount"] = "1" })
		}, "amount differs"},
		{"memo deposits refused", func(r map[string]any) (int, any) {
			return echoQuote(r, func(q, _ map[string]any) { q["depositMemo"] = "123" })
		}, "memo"},
		{"provider rejects amount", func(map[string]any) (int, any) {
			return 400, map[string]any{"message": "Amount is too low for bridge, try at least 0.001"}
		}, "Amount is too low"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fake{t: t, quoteHook: tt.hook}
			srv := f.server()
			defer srv.Close()
			c := New(Config{BaseURL: srv.URL, APIKey: "k-123", Referral: "max"})

			q, err := c.Quote(ctx, baseReq())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.sawAPIKey != "k-123" {
				t.Error("API key header not sent")
			}
			if q.DepositAddress == "" || q.AmountOut.String() != "2490000000" || q.MinAmountOut.String() != "2465100000" ||
				q.Signature == "" || len(q.Raw) == 0 || q.TimeEstimate != 45*time.Second {
				t.Errorf("quote %+v", q)
			}
			if err := routing.ValidateQuote(baseReq(), q, time.Now(), time.Now().Add(15*time.Minute)); err != nil {
				t.Errorf("valid quote fails validation: %v", err)
			}
		})
	}
}

func TestQuote_RejectionIsTyped(t *testing.T) {
	f := &fake{t: t, quoteHook: func(map[string]any) (int, any) { return 400, map[string]any{"message": "no route"} }}
	srv := f.server()
	defer srv.Close()
	_, err := New(Config{BaseURL: srv.URL}).Quote(context.Background(), baseReq())
	var rej *routing.QuoteRejectedError
	if !errors.As(err, &rej) || rej.Reason != "no route" {
		t.Fatalf("err = %v", err)
	}
}

func TestStatus(t *testing.T) {
	ctx := context.Background()
	f := &fake{t: t, statusBody: `{"status":"SUCCESS","updatedAt":"2026-01-01T00:01:00Z","swapDetails":{
		"intentHashes":[],"nearTxHashes":[],"amountOut":"2489000000",
		"originChainTxHashes":[{"hash":"0xabc","explorerUrl":"https://basescan.org/tx/0xabc"}],
		"destinationChainTxHashes":[{"hash":"5sig","explorerUrl":"https://solscan.io/tx/5sig"}]}}`}
	srv := f.server()
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL})

	st, err := c.Status(ctx, "0x9999")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != routing.StateSuccess || st.AmountOut.String() != "2489000000" || len(st.DestinationTxs) != 1 || st.DestinationTxs[0].Hash != "5sig" {
		t.Fatalf("status %+v", st)
	}

	f.statusCode, f.statusBody = 404, `{"message":"not found"}`
	if _, err := c.Status(ctx, "0x9999"); !errors.Is(err, routing.ErrDepositNotFound) {
		t.Fatalf("404 err = %v", err)
	}
}
