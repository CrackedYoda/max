package payments

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/resolver"
	"github.com/max/max-core/internal/routing"
)

const (
	solUSDC       = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	evmDeposit    = "0x9999999999999999999999999999999999999999"
	solDeposit    = "7Xw2uL7ZyyAjLwxmGjKyYDVWZLgRsVw8tZ1UFYyHvq5p"
	ethBaseAsset  = "nep141:base.omft.near"
	usdcBaseAsset = "nep141:base-usdc"
	usdcSolAsset  = "nep141:sol-usdc"
)

type fakeProvider struct {
	mu        sync.Mutex
	assets    map[string]routing.ProviderAsset
	quoteFn   func(routing.QuoteRequest) (routing.Quote, error)
	lastQuote routing.QuoteRequest
	status    routing.ExecutionStatus
	statusErr error
	deposits  []string
}

func assetKey(r routing.AssetRef) string { return r.Chain.Key + "|" + string(r.Kind) + "|" + r.Address }

func newFakeProvider() *fakeProvider {
	f := &fakeProvider{assets: map[string]routing.ProviderAsset{
		"base|native|":            {ID: ethBaseAsset, Decimals: 18, Symbol: "ETH"},
		"base|token|" + usdcBase:  {ID: usdcBaseAsset, Decimals: 6, Symbol: "USDC"},
		"solana|native|":          {ID: "nep141:sol.omft.near", Decimals: 9, Symbol: "SOL"},
		"solana|token|" + solUSDC: {ID: usdcSolAsset, Decimals: 6, Symbol: "USDC"},
		"arbitrum|native|":        {ID: "nep141:arb.omft.near", Decimals: 18, Symbol: "ETH"},
	}}
	f.quoteFn = f.defaultQuote
	return f
}

// defaultQuote prices everything 1:2500 ETH→USDC with exactly the requested slippage.
func (f *fakeProvider) defaultQuote(req routing.QuoteRequest) (routing.Quote, error) {
	deposit := evmDeposit
	if req.Origin.Chain.Namespace == chains.NamespaceSolana {
		deposit = solDeposit
	}
	q := routing.Quote{
		Provider: "near_intents", OriginAssetID: "o", DestinationAssetID: "d", DepositAddress: deposit,
		AmountInUSD: "250.00", AmountOutUSD: "249.00", Deadline: req.Deadline,
		TimeEstimate: 40 * time.Second, Signature: "ed25519:sig", Raw: []byte(`{"quote":{}}`),
	}
	if req.SwapType == routing.ExactInput {
		q.AmountIn = new(big.Int).Set(req.Amount)
		q.AmountOut = big.NewInt(2_490_000_000)
	} else {
		q.AmountIn = big.NewInt(10_000_000_000_000_000) // 0.01 ETH
		q.AmountOut = new(big.Int).Set(req.Amount)
	}
	q.MinAmountOut = applyBpsTest(q.AmountOut, 10_000-req.SlippageBps)
	if req.SwapType == routing.ExactOutput {
		q.MinAmountOut = new(big.Int).Set(req.Amount)
	}
	return q, nil
}

func applyBpsTest(v *big.Int, bps int) *big.Int {
	out := new(big.Int).Mul(v, big.NewInt(int64(bps)))
	return out.Quo(out, big.NewInt(10_000))
}

func (f *fakeProvider) Name() string { return "near_intents" }
func (f *fakeProvider) Asset(_ context.Context, r routing.AssetRef) (routing.ProviderAsset, error) {
	a, ok := f.assets[assetKey(r)]
	if !ok {
		return routing.ProviderAsset{}, routing.ErrRouteUnavailable
	}
	return a, nil
}
func (f *fakeProvider) Quote(_ context.Context, req routing.QuoteRequest) (routing.Quote, error) {
	f.mu.Lock()
	f.lastQuote = req
	f.mu.Unlock()
	return f.quoteFn(req)
}
func (f *fakeProvider) SubmitDeposit(_ context.Context, addr, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deposits = append(f.deposits, addr)
	return nil
}
func (f *fakeProvider) Status(context.Context, string) (routing.ExecutionStatus, error) {
	return f.status, f.statusErr
}

func crossIn(chain, asset, amount, sender, destChain, destAsset string) PrepareInput {
	in := prepareIn(chain, asset, amount, sender)
	in.DestinationChain, in.DestinationAsset = destChain, destAsset
	return in
}

func TestPrepareCrossChain(t *testing.T) {
	ctx := context.Background()

	t.Run("ETH on Base to USDC on Solana", func(t *testing.T) {
		h := newHarness()
		res, err := h.svc.Prepare(ctx, crossIn("base", "native", "1", evmSender, "solana", solUSDC))
		if err != nil {
			t.Fatal(err)
		}
		p := res.Payment
		r := p.CrossChain
		if p.Status != StatusQuoted || p.Route != RouteCrossChain || r == nil {
			t.Fatalf("payment %+v", p)
		}
		if p.RecipientAddress != solRecipient || r.DepositAddress != evmDeposit || r.RefundTo != evmSender {
			t.Fatalf("addresses: recipient=%s deposit=%s refund=%s", p.RecipientAddress, r.DepositAddress, r.RefundTo)
		}
		if r.MinAmountOut.String() != "2465100000" || !r.DestinationAsset.Verified || r.DestinationAsset.Symbol != "USDC" {
			t.Fatalf("route %+v", r)
		}
		e := res.Instructions.EVM
		if !strings.EqualFold(e.To, evmDeposit) || e.ValueHex != "0xde0b6b3a7640000" {
			t.Fatalf("sender must pay the deposit address exactly amountIn: %+v", e)
		}
		q := h.prov.lastQuote
		if q.Recipient != solRecipient || !strings.EqualFold(q.RefundTo, evmSender) || q.SlippageBps != 100 || q.SwapType != routing.ExactInput {
			t.Fatalf("quote request %+v", q)
		}
		if !strings.Contains(strings.Join(res.Warnings, " "), "receives at least 2465.1 USDC on Solana") {
			t.Fatalf("missing sender disclosure: %v", res.Warnings)
		}
	})

	t.Run("exact output: recipient gets exactly 25 USDC", func(t *testing.T) {
		h := newHarness()
		in := crossIn("base", "native", "25", evmSender, "solana", solUSDC)
		in.AmountType = "output"
		res, err := h.svc.Prepare(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if h.prov.lastQuote.Amount.String() != "25000000" || h.prov.lastQuote.SwapType != routing.ExactOutput {
			t.Fatalf("quote request %+v", h.prov.lastQuote)
		}
		if res.Payment.Amount.String() != "10000000000000000" || res.Payment.CrossChain.RequestedAmount.String() != "25000000" {
			t.Fatalf("amounts: pay=%s requested=%s", res.Payment.Amount, res.Payment.CrossChain.RequestedAmount)
		}
	})

	t.Run("SOL USDC deposit goes to the deposit address's token account", func(t *testing.T) {
		h := newHarness()
		res, err := h.svc.Prepare(ctx, crossIn("solana", solUSDC, "100", solSender, "base", "native"))
		if err != nil {
			t.Fatal(err)
		}
		wantATA, _ := chains.AssociatedTokenAddress(solDeposit, solUSDC, chains.SPLTokenProgram)
		if s := res.Instructions.Solana; s.DestinationTokenAccount != wantATA || s.To != solDeposit {
			t.Fatalf("instruction %+v", s)
		}
	})

	t.Run("recipient settlement preference is used when they have no wallet on the sender's chain", func(t *testing.T) {
		h := newHarness()
		delete(h.res.addrs, chains.NamespaceEVM)
		h.res.settlement = &resolver.Settlement{Chain: mustChain("solana"), Asset: resolver.Asset{Kind: resolver.AssetToken, Address: solUSDC}}
		res, err := h.svc.Prepare(ctx, prepareIn("base", "native", "1", evmSender))
		if err != nil {
			t.Fatal(err)
		}
		if res.Payment.Route != RouteCrossChain || res.Payment.CrossChain.DestinationChain.Key != "solana" {
			t.Fatalf("expected route to Solana USDC, got %+v", res.Payment)
		}
	})

	t.Run("locks the earlier of refund deadline and deposit-address expiry", func(t *testing.T) {
		h := newHarness()
		h.prov.quoteFn = func(r routing.QuoteRequest) (routing.Quote, error) {
			q, _ := h.prov.defaultQuote(r)
			q.Deadline = r.Deadline.Add(72 * time.Hour) // what 1Click returns in practice
			return q, nil
		}
		res, err := h.svc.Prepare(ctx, crossIn("base", "native", "1", evmSender, "solana", solUSDC))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := res.Payment.CrossChain.QuoteDeadline, h.now.Add(h.svc.cfg.QuoteDeadline); !got.Equal(want) {
			t.Fatalf("quote deadline = %s, want requested refund deadline %s", got, want)
		}
	})

	t.Run("same chain and asset as destination stays direct", func(t *testing.T) {
		h := newHarness()
		res, err := h.svc.Prepare(ctx, crossIn("base", usdcBase, "5", evmSender, "base", chains.ChecksumEVMAddress(usdcBase)))
		if err != nil {
			t.Fatal(err)
		}
		if res.Payment.Route != RouteDirect || h.prov.lastQuote.Amount != nil {
			t.Fatal("should not have quoted a route")
		}
	})
}

func TestPrepareCrossChain_Rejections(t *testing.T) {
	ctx := context.Background()
	tooWide := 301

	tests := []struct {
		name    string
		setup   func(*harness, *PrepareInput)
		wantErr error
		wantMsg string
	}{
		{"kill switch", func(h *harness, _ *PrepareInput) { h.svc.cfg.RoutingEnabled = false }, ErrRoutingDisabled, ""},
		{"no wallet and no preference", func(h *harness, in *PrepareInput) {
			in.DestinationChain = ""
			delete(h.res.addrs, chains.NamespaceEVM)
		}, resolver.ErrNoDestination, ""},
		{"testnet", func(h *harness, in *PrepareInput) { in.Chain = "base-sepolia" }, ErrRouteUnavailable, "mainnet"},
		{"asset the provider cannot route", func(h *harness, _ *PrepareInput) {
			delete(h.prov.assets, "solana|token|"+solUSDC)
		}, ErrRouteUnavailable, ""},
		{"provider rejects with a reason", func(h *harness, _ *PrepareInput) {
			h.prov.quoteFn = func(routing.QuoteRequest) (routing.Quote, error) {
				return routing.Quote{}, &routing.QuoteRejectedError{Reason: "Amount is too low for bridge"}
			}
		}, ErrQuoteRejected, "too low"},
		{"provider outage", func(h *harness, _ *PrepareInput) {
			h.prov.quoteFn = func(routing.QuoteRequest) (routing.Quote, error) { return routing.Quote{}, errors.New("timeout") }
		}, ErrRoutingProvider, ""},
		{"quote widens slippage beyond what sender agreed", func(h *harness, _ *PrepareInput) {
			h.prov.quoteFn = func(r routing.QuoteRequest) (routing.Quote, error) {
				q, _ := h.prov.defaultQuote(r)
				q.MinAmountOut = big.NewInt(1)
				return q, nil
			}
		}, ErrQuoteInvalid, ""},
		{"deposit address is the sender", func(h *harness, _ *PrepareInput) {
			h.prov.quoteFn = func(r routing.QuoteRequest) (routing.Quote, error) {
				q, _ := h.prov.defaultQuote(r)
				q.DepositAddress = evmSender
				return q, nil
			}
		}, ErrQuoteInvalid, ""},
		{"over the USD cap", func(h *harness, _ *PrepareInput) {
			h.prov.quoteFn = func(r routing.QuoteRequest) (routing.Quote, error) {
				q, _ := h.prov.defaultQuote(r)
				q.AmountInUSD = "5000.01"
				return q, nil
			}
		}, ErrRouteLimit, "$1000"},
		{"slippage too wide", func(h *harness, in *PrepareInput) { in.SlippageBps = &tooWide }, ErrInvalidSlippage, ""},
		{"provider and chain disagree on source decimals", func(h *harness, in *PrepareInput) {
			in.Asset = usdcBase
			h.prov.assets["base|token|"+usdcBase] = routing.ProviderAsset{ID: usdcBaseAsset, Decimals: 18}
		}, ErrRouteUnavailable, "decimals"},
		{"recipient has no wallet on the destination chain", func(h *harness, _ *PrepareInput) {
			delete(h.res.addrs, chains.NamespaceSolana)
		}, resolver.ErrNoDestination, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness()
			in := crossIn("base", "native", "1", evmSender, "solana", solUSDC)
			tt.setup(h, &in)
			_, err := h.svc.Prepare(ctx, in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("err %q missing %q", err, tt.wantMsg)
			}
		})
	}
}

func TestPrepareCrossChain_Idempotency(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	in := crossIn("base", "native", "1", evmSender, "solana", solUSDC)
	first, err := h.svc.Prepare(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.svc.Prepare(ctx, in)
	if err != nil || !again.Replayed || again.Payment.ID != first.Payment.ID {
		t.Fatalf("replay: %v %+v", err, again)
	}
	in.DestinationChain = "arbitrum"
	if _, err := h.svc.Prepare(ctx, in); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different destination with same key: %v", err)
	}
}

func quotedAndSubmitted(t *testing.T, h *harness) Payment {
	t.Helper()
	ctx := context.Background()
	res, err := h.svc.Prepare(ctx, crossIn("base", "native", "1", evmSender, "solana", solUSDC))
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.svc.Submit(ctx, SubmitInput{PaymentID: res.Payment.ID, TxHash: evmTxHash})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSubmitCrossChain(t *testing.T) {
	ctx := context.Background()

	t.Run("submit from QUOTED notifies the provider", func(t *testing.T) {
		h := newHarness()
		p := quotedAndSubmitted(t, h)
		if p.Status != StatusSubmitted || len(h.prov.deposits) != 1 || h.prov.deposits[0] != evmDeposit {
			t.Fatalf("status %s deposits %v", p.Status, h.prov.deposits)
		}
	})

	t.Run("late submit after the quote deadline is refused", func(t *testing.T) {
		h := newHarness()
		res, _ := h.svc.Prepare(ctx, crossIn("base", "native", "1", evmSender, "solana", solUSDC))
		h.repo.Apply(ctx, res.Payment.ID, Transition{From: StatusQuoted, To: StatusExpired})
		h.now = res.Payment.CrossChain.QuoteDeadline.Add(time.Second)
		if _, err := h.svc.Submit(ctx, SubmitInput{PaymentID: res.Payment.ID, TxHash: evmTxHash}); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestProcessCrossChain(t *testing.T) {
	ctx := context.Background()
	minOut := big.NewInt(2_465_100_000)

	type step struct {
		obs    *Observation
		status *routing.ExecutionStatus
		now    func(p Payment) time.Time
		want   Status
	}
	st := func(s routing.ExecutionState, out int64) *routing.ExecutionStatus {
		es := &routing.ExecutionStatus{State: s}
		if out > 0 {
			es.AmountOut = big.NewInt(out)
		}
		return es
	}
	final := &Observation{Found: true, Confirmations: 2, Final: true}

	tests := []struct {
		name  string
		steps []step
	}{
		{"happy path: deposit confirmed, provider settles", []step{
			{obs: &Observation{Found: true, Confirmations: 1}, want: StatusConfirming},
			{obs: final, want: StatusConfirming},
			{status: st(routing.StateProcessing, 0), want: StatusConfirming},
			{status: st(routing.StateSuccess, 2_480_000_000), want: StatusCompleted},
		}},
		{"settles below the quoted minimum", []step{
			{obs: final, want: StatusConfirming},
			{status: st(routing.StateSuccess, minOut.Int64()-1), want: StatusManualReview},
		}},
		{"provider refunds", []step{
			{obs: final, want: StatusConfirming},
			{status: st(routing.StateRefunded, 0), want: StatusRefunded},
		}},
		{"incomplete deposit then refund", []step{
			{obs: final, want: StatusConfirming},
			{status: st(routing.StateIncompleteDeposit, 0), want: StatusRefundPending},
			{status: st(routing.StateIncompleteDeposit, 0), want: StatusRefundPending},
			{status: st(routing.StateRefunded, 0), want: StatusRefunded},
		}},
		{"provider failure needs a human", []step{
			{obs: final, want: StatusConfirming},
			{status: st(routing.StateFailed, 0), want: StatusManualReview},
		}},
		{"deposit reverted", []step{
			{obs: &Observation{Found: true, Reverted: true}, want: StatusFailed},
		}},
		{"tx paid somewhere else and provider saw nothing", []step{
			{obs: &Observation{Found: true, Mismatch: "wrong recipient"}, status: st(routing.StatePendingDeposit, 0), want: StatusFailed},
		}},
		{"wrong amount reached the deposit address", []step{
			{obs: &Observation{Found: true, Mismatch: "wrong amount"}, status: st(routing.StateKnownDepositTx, 0), want: StatusRefundPending},
			{status: st(routing.StateRefunded, 0), want: StatusRefunded},
		}},
		{"provider silent long after the deadline", []step{
			{obs: final, want: StatusConfirming},
			{status: st(routing.StateProcessing, 0), now: func(p Payment) time.Time {
				return p.CrossChain.QuoteDeadline.Add(3 * time.Hour)
			}, want: StatusManualReview},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness()
			p := quotedAndSubmitted(t, h)
			for i, s := range tt.steps {
				h.gw.obs = Observation{}
				if s.obs != nil {
					h.gw.obs = *s.obs
				}
				h.prov.status = routing.ExecutionStatus{State: routing.StatePendingDeposit}
				if s.status != nil {
					h.prov.status = *s.status
				}
				if s.now != nil {
					h.now = s.now(p)
				}
				if err := h.svc.Process(ctx, p); err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				p, _ = h.repo.Get(ctx, p.ID)
				if p.Status != s.want {
					t.Fatalf("step %d: status = %s (%s), want %s", i, p.Status, p.StatusReason, s.want)
				}
			}
			if p.Status == StatusCompleted {
				if p.CrossChain.AmountOutActual == nil || p.CrossChain.ProviderStatus != "SUCCESS" {
					t.Fatalf("settlement not recorded: %+v", p.CrossChain)
				}
			}
		})
	}
}

func TestProcessCrossChain_ProviderNotYetAware(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	p := quotedAndSubmitted(t, h)
	h.gw.obs = Observation{Found: true, Confirmations: 2, Final: true}
	_ = h.svc.Process(ctx, p)
	p, _ = h.repo.Get(ctx, p.ID)

	h.prov.statusErr = routing.ErrDepositNotFound
	if err := h.svc.Process(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.repo.Get(ctx, p.ID); got.Status != StatusConfirming {
		t.Fatalf("status = %s", got.Status)
	}
}

var _ = uuid.Nil

type listingProvider struct {
	fakeProvider
	assets []routing.ListedAsset
}

func (l *listingProvider) Assets(context.Context) ([]routing.ListedAsset, error) {
	return l.assets, nil
}

func TestReceivableAssets(t *testing.T) {
	baseUSDC := chains.VerifiedTokensFor("base")[0]
	prov := &listingProvider{assets: []routing.ListedAsset{
		{ChainKey: "base", Kind: routing.AssetToken, Address: "0x4200000000000000000000000000000000000042", Symbol: "ZZZ", Decimals: 18},
		{ChainKey: "base", Kind: routing.AssetToken, Address: strings.ToLower(baseUSDC.Address), Symbol: "USDC", Decimals: baseUSDC.Decimals},
		{ChainKey: "base", Kind: routing.AssetNative, Symbol: "ETH", Decimals: 18},
		{ChainKey: "bitcoin", Kind: routing.AssetNative, Symbol: "BTC(OMNI)", Decimals: 8},
		{ChainKey: "bitcoin", Kind: routing.AssetNative, Symbol: "BTC", Decimals: 8},
		{ChainKey: "base-sepolia", Kind: routing.AssetNative, Symbol: "ETH", Decimals: 18},
		{ChainKey: "nowhere", Kind: routing.AssetNative, Symbol: "X", Decimals: 1},
	}}
	svc := &service{provider: prov, cfg: Config{RoutingEnabled: true}}
	got, err := svc.ReceivableAssets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range got {
		names = append(names, fmt.Sprintf("%s/%s/%v", a.Chain.Key, a.Symbol, a.Verified))
	}
	want := []string{"base/ETH/true", "base/USDC/true", "base/ZZZ/false", "bitcoin/BTC/true"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", names, want)
	}
	if want := chains.DisplayAddress(chains.NamespaceEVM, baseUSDC.Address); got[1].Address != want {
		t.Errorf("USDC address = %s, want display form %s", got[1].Address, want)
	}

	svc.cfg.RoutingEnabled = false
	if got, _ := svc.ReceivableAssets(context.Background()); len(got) != 0 {
		t.Errorf("routing off still lists %d assets", len(got))
	}
}
