package holdings

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
)

const (
	evmOwner = "0x52908400098527886e0f7030069857d2e4169ee7"
	usdcBase = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	spamBase = "0x1111111111111111111111111111111111111111"
	solOwner = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"
	bonk     = "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"
)

func chain(t *testing.T, key string) chains.Chain {
	t.Helper()
	c, ok := chains.Lookup(key)
	if !ok {
		t.Fatalf("no chain %s", key)
	}
	return c
}

// fakeAlchemy serves pages in order and records each request body.
type fakeAlchemy struct {
	t     *testing.T
	pages []string
	reqs  []map[string]any
}

func (f *fakeAlchemy) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/data/v1/test-key/assets/tokens/by-address") {
			f.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.reqs = append(f.reqs, body)
		w.Write([]byte(f.pages[min(len(f.reqs), len(f.pages))-1]))
	}))
}

const basePage = `{"data":{"tokens":[
 {"address":"0x52908400098527886e0f7030069857d2e4169ee7","network":"base-mainnet","tokenAddress":null,"tokenBalance":"0xde0b6b3a7640000",
  "tokenMetadata":{"decimals":null,"logo":null,"name":null,"symbol":null},"tokenPrices":[{"currency":"usd","value":"2500.5","lastUpdatedAt":"2026-11-10T00:00:00Z"}],"error":null},
 {"address":"0x52908400098527886e0f7030069857d2e4169ee7","network":"base-mainnet","tokenAddress":"0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913","tokenBalance":"0x0000000000000000000000000000000000000000000000000000000001312d00",
  "tokenMetadata":{"decimals":6,"logo":"https://logo/usdc.png","name":"USD Coin","symbol":"USDC"},"tokenPrices":[{"currency":"usd","value":"1.0001","lastUpdatedAt":"2026-11-10T00:00:00Z"}],"error":null},
 {"address":"0x52908400098527886e0f7030069857d2e4169ee7","network":"base-mainnet","tokenAddress":"0x1111111111111111111111111111111111111111","tokenBalance":"0x3635c9adc5dea00000",
  "tokenMetadata":{"decimals":18,"logo":null,"name":"Claim Rewards At scam.xyz","symbol":"FREE"},"tokenPrices":[],"error":null},
 {"address":"0x52908400098527886e0f7030069857d2e4169ee7","network":"base-mainnet","tokenAddress":"0x2222222222222222222222222222222222222222","tokenBalance":"0x0",
  "tokenMetadata":{"decimals":18,"logo":null,"name":"Empty","symbol":"ZERO"},"tokenPrices":[],"error":null},
 {"address":"0x52908400098527886e0f7030069857d2e4169ee7","network":"base-mainnet","tokenAddress":"0x3333333333333333333333333333333333333333","tokenBalance":null,
  "tokenMetadata":{"decimals":18,"logo":null,"name":"Broken","symbol":"ERR"},"tokenPrices":[],"error":"failed to fetch balance"}
 ],"pageKey":"next"}}`

const basePage2 = `{"data":{"tokens":[
 {"address":"0x52908400098527886e0f7030069857d2e4169ee7","network":"base-mainnet","tokenAddress":"0x4444444444444444444444444444444444444444","tokenBalance":"0x4563918244f40000",
  "tokenMetadata":{"decimals":18,"logo":null,"name":"Degen","symbol":"DEGEN"},"tokenPrices":[{"currency":"usd","value":"0.5"}],"error":null},
 {"address":"0x52908400098527886e0f7030069857d2e4169ee7","network":"base-mainnet","tokenAddress":"0x5555555555555555555555555555555555555555","tokenBalance":"0xde0b6b3a7640000",
  "tokenMetadata":{"decimals":18,"logo":null,"name":"Scam","symbol":"SCAM"},"tokenPrices":[{"currency":"usd","value":"1000000"}],"error":null}
 ],"pageKey":null}}`

func newService(t *testing.T, f *fakeAlchemy) (*Service, func()) {
	srv := f.server()
	src := NewAlchemy(AlchemyConfig{APIKey: "test-key", BaseURL: srv.URL})
	return NewService(src, nil), srv.Close
}

func TestList_EVMFromAlchemy(t *testing.T) {
	f := &fakeAlchemy{t: t, pages: []string{basePage, basePage2}}
	svc, done := newService(t, f)
	defer done()

	got, err := svc.List(context.Background(), "base", "0x52908400098527886E0F7030069857D2E4169EE7")
	if err != nil {
		t.Fatal(err)
	}
	var syms []string
	for _, h := range got {
		syms = append(syms, h.Symbol)
	}
	// Native first, verified USDC, then unverified by value (SCAM's made-up $1M > DEGEN $2.50), then unpriced; zero and errored dropped.
	if strings.Join(syms, ",") != "ETH,USDC,SCAM,DEGEN,FREE" {
		t.Fatalf("order = %v", syms)
	}
	eth, usdc, free := got[0], got[1], got[4]
	if eth.Kind != KindNative || eth.Address != "" || eth.Decimals != 18 || eth.Name != "Base" || eth.Balance.String() != "1000000000000000000" || eth.ValueUSD != "2500.50" || !eth.Verified {
		t.Errorf("native = %+v", eth)
	}
	if usdc.Kind != KindToken || usdc.Address != usdcBase || usdc.Balance.String() != "20000000" || usdc.PriceUSD != "1.0001" || usdc.ValueUSD != "20.00" || !usdc.Verified || usdc.Logo != "https://logo/usdc.png" {
		t.Errorf("usdc = %+v", usdc)
	}
	if free.Verified || free.PriceUSD != "" || free.ValueUSD != "" || free.Balance.String() != "1000000000000000000000" {
		t.Errorf("spam token = %+v", free)
	}
	if len(f.reqs) != 2 || f.reqs[1]["pageKey"] != "next" {
		t.Fatalf("pagination: %v", f.reqs)
	}
	addr := f.reqs[0]["addresses"].([]any)[0].(map[string]any)
	if addr["address"] != chains.ChecksumEVMAddress(evmOwner) || addr["networks"].([]any)[0] != "base-mainnet" {
		t.Fatalf("request %v", f.reqs[0])
	}
}

func TestList_SolanaNativeAddedWhenMissing(t *testing.T) {
	page := `{"data":{"tokens":[
 {"address":"` + solOwner + `","network":"solana-mainnet","tokenAddress":"` + bonk + `","tokenBalance":"120450000",
  "tokenMetadata":{"decimals":5,"logo":null,"name":"Bonk","symbol":"Bonk"},"tokenPrices":[{"currency":"usd","value":"0.0000153"}],"error":null},
 {"address":"` + solOwner + `","network":"solana-mainnet","tokenAddress":"2PGzsg8yeAXRA6i9WYztBs3YWdbr4jRbixM1oTZEArX7","tokenBalance":"0x1",
  "tokenMetadata":{"decimals":0,"logo":"https://nft/y00t.png","name":"y00t #4718","symbol":"Y00T"},"tokenPrices":[],"error":null},
 {"address":"` + solOwner + `","network":"solana-mainnet","tokenAddress":"13vudpRiKaMRg1fif48NkAQF3o4B9EsBbWwfRKyc64VE","tokenBalance":"0x186a0",
  "tokenMetadata":{"decimals":6,"logo":null,"name":null,"symbol":null},"tokenPrices":[],"error":null}
 ],"pageKey":null}}`
	f := &fakeAlchemy{t: t, pages: []string{page}}
	svc, done := newService(t, f)
	defer done()
	got, err := svc.List(context.Background(), "solana", solOwner)
	if err != nil {
		t.Fatal(err)
	}
	// The zero-decimal NFT is dropped; the nameless token is labelled by its mint.
	if len(got) != 3 || got[0].Symbol != "SOL" || got[0].Balance.Sign() != 0 || got[1].Address != bonk || got[1].ValueUSD != "0.02" || got[2].Symbol != "13vu…64VE" {
		t.Fatalf("got %+v", got)
	}
	if f.reqs[0]["addresses"].([]any)[0].(map[string]any)["networks"].([]any)[0] != "solana-mainnet" {
		t.Fatalf("network slug: %v", f.reqs[0])
	}
}

func TestList_CachesPerChainAndAddress(t *testing.T) {
	f := &fakeAlchemy{t: t, pages: []string{basePage2}}
	svc, done := newService(t, f)
	defer done()
	now := time.Now()
	svc.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if _, err := svc.List(context.Background(), "base", evmOwner); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(f.reqs))
	}
	now = now.Add(21 * time.Second)
	_, _ = svc.List(context.Background(), "base", evmOwner)
	if len(f.reqs) != 2 {
		t.Fatalf("cache did not expire: %d", len(f.reqs))
	}
}

func TestList_FallsBackToNativeRPC(t *testing.T) {
	svc := NewService(nil, map[string]NativeReader{
		"base-sepolia": func(context.Context, string) (*big.Int, error) { return big.NewInt(5e17), nil },
	})
	got, err := svc.List(context.Background(), "base-sepolia", evmOwner)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != KindNative || got[0].Balance.Int64() != 5e17 || got[0].PriceUSD != "" {
		t.Fatalf("got %+v", got)
	}
	if _, err := svc.List(context.Background(), "base", evmOwner); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no source, no RPC: err = %v", err)
	}
}

func TestList_Rejects(t *testing.T) {
	svc := NewService(nil, nil)
	for _, tc := range []struct {
		chain, addr string
		want        error
	}{
		{"bitcoin", "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", ErrUnsupportedChain},
		{"nope", evmOwner, ErrUnsupportedChain},
		{"base", solOwner, ErrInvalidAddress},
		{"solana", evmOwner, ErrInvalidAddress},
	} {
		if _, err := svc.List(context.Background(), tc.chain, tc.addr); !errors.Is(err, tc.want) {
			t.Errorf("%s %s: err = %v, want %v", tc.chain, tc.addr, err, tc.want)
		}
	}
}

func TestList_ProviderErrorIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"boom"}}`, http.StatusInternalServerError)
	}))
	defer srv.Close()
	svc := NewService(NewAlchemy(AlchemyConfig{APIKey: "test-key", BaseURL: srv.URL}), nil)
	if _, err := svc.List(context.Background(), "base", evmOwner); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestAlchemyNetworks(t *testing.T) {
	want := map[string]string{
		"ethereum": "eth-mainnet", "base": "base-mainnet", "arbitrum": "arb-mainnet", "optimism": "opt-mainnet",
		"polygon": "matic-mainnet", "bnb": "bnb-mainnet", "avalanche": "avax-mainnet", "solana": "solana-mainnet",
	}
	for k, v := range want {
		if alchemyNetworks[k] != v {
			t.Errorf("%s = %q, want %q", k, alchemyNetworks[k], v)
		}
	}
	for _, c := range chains.Supported {
		if _, ok := alchemyNetworks[c.Key]; ok && (c.Testnet || c.ReceiveOnly) {
			t.Errorf("%s must not map to Alchemy", c.Key)
		}
	}
}

func TestResponseShape(t *testing.T) {
	c := chain(t, "base")
	b, _ := json.Marshal(toResponse(c, []Holding{
		{Kind: KindNative, Symbol: "ETH", Decimals: 18, Balance: big.NewInt(1500000000000000000), PriceUSD: "2500", ValueUSD: "3750.00", Verified: true},
		{Kind: KindToken, Address: usdcBase, Symbol: "FREE", Decimals: 18, Balance: big.NewInt(1)},
	}))
	s := string(b)
	for _, want := range []string{
		`"balance":{"raw":"1500000000000000000","display":"1.5"},"priceUsd":"2500","valueUsd":"3750.00","verified":true`,
		`"address":"0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"`,
		`"priceUsd":null,"valueUsd":null,"verified":false`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
}
