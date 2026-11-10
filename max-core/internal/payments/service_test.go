package payments

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/resolver"
	"github.com/max/max-core/internal/routing"
	"github.com/max/max-core/internal/shared"
)

type memRepo struct {
	mu       sync.Mutex
	payments map[uuid.UUID]Payment
	events   map[uuid.UUID][]Event
}

func newMemRepo() *memRepo {
	return &memRepo{payments: map[uuid.UUID]Payment{}, events: map[uuid.UUID][]Event{}}
}

func (m *memRepo) Create(_ context.Context, p Payment) (Payment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.payments {
		if e.SenderAddress == p.SenderAddress && e.IdempotencyKey == p.IdempotencyKey {
			return Payment{}, errDuplicateIdempotency
		}
	}
	p.ID, p.CreatedAt = uuid.New(), time.Now()
	m.payments[p.ID] = p
	m.events[p.ID] = append(m.events[p.ID], Event{Type: "created", ToStatus: p.Status})
	return p, nil
}

func (m *memRepo) GetByIdempotencyKey(_ context.Context, sender, key string) (Payment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.payments {
		if p.SenderAddress == sender && p.IdempotencyKey == key {
			return p, nil
		}
	}
	return Payment{}, ErrPaymentNotFound
}

func (m *memRepo) Get(_ context.Context, id uuid.UUID) (Payment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.payments[id]
	if !ok {
		return Payment{}, ErrPaymentNotFound
	}
	return p, nil
}

func (m *memRepo) Events(_ context.Context, id uuid.UUID) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events[id], nil
}

func (m *memRepo) ListBySender(context.Context, uuid.UUID, int32) ([]Payment, error) { return nil, nil }
func (m *memRepo) ListReceived(context.Context, uuid.UUID, int32) ([]Payment, error) { return nil, nil }

func (m *memRepo) Apply(_ context.Context, id uuid.UUID, t Transition) (Payment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.payments[id]
	if p.Status != t.From {
		return Payment{}, ErrStaleTransition
	}
	if t.TxHash != nil {
		for oid, o := range m.payments {
			if oid != id && o.Chain.Key == p.Chain.Key && o.TxHash == *t.TxHash {
				return Payment{}, ErrTxAlreadyUsed
			}
		}
		p.TxHash = *t.TxHash
	}
	p.Status, p.StatusReason = t.To, t.Reason
	if t.Confirmations != nil {
		p.Confirmations = *t.Confirmations
	}
	if t.SubmittedAt != nil {
		p.SubmittedAt = t.SubmittedAt
	}
	if t.CompletedAt != nil {
		p.CompletedAt = t.CompletedAt
	}
	m.payments[id] = p
	m.events[id] = append(m.events[id], Event{Type: t.Event, FromStatus: t.From, ToStatus: t.To})
	return p, nil
}

func (m *memRepo) Reschedule(_ context.Context, id uuid.UUID, _ Status, _ time.Duration, conf int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.payments[id]
	p.Confirmations = conf
	m.payments[id] = p
	return nil
}

func (m *memRepo) MarkDepositConfirmed(_ context.Context, id uuid.UUID, status Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.payments[id]
	if p.CrossChain == nil || p.CrossChain.DepositConfirmedAt != nil {
		return nil
	}
	r := *p.CrossChain
	now := time.Now()
	r.DepositConfirmedAt = &now
	p.CrossChain = &r
	m.payments[id] = p
	m.events[id] = append(m.events[id], Event{Type: "deposit_confirmed", FromStatus: status, ToStatus: status})
	return nil
}

func (m *memRepo) UpdateRouteProgress(_ context.Context, id uuid.UUID, pr RouteProgress) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.payments[id]
	r := *p.CrossChain
	r.ProviderStatus, r.DestinationTxs = pr.ProviderStatus, pr.DestinationTxs
	if pr.AmountOutActual != nil {
		r.AmountOutActual = pr.AmountOutActual
	}
	if pr.RefundedAmount != nil {
		r.RefundedAmount = pr.RefundedAmount
	}
	if pr.RefundReason != "" {
		r.RefundReason = pr.RefundReason
	}
	p.CrossChain = &r
	m.payments[id] = p
	return nil
}

func (m *memRepo) LeaseDue(context.Context, int32, time.Duration) ([]Payment, error) { return nil, nil }
func (m *memRepo) ExpireAwaiting(context.Context) ([]uuid.UUID, error)               { return nil, nil }

type fakeResolver struct {
	version    int64
	addrs      map[chains.Namespace]string
	settlement *resolver.Settlement
}

func (f *fakeResolver) Settlement(context.Context, string) (*resolver.Settlement, error) {
	return f.settlement, nil
}

func (f *fakeResolver) Resolve(_ context.Context, name, chainKey, asset string) (resolver.Resolution, error) {
	chain, ok := chains.Lookup(chainKey)
	if !ok {
		return resolver.Resolution{}, resolver.ErrUnsupportedChain
	}
	if strings.TrimPrefix(strings.ToLower(name), "max.") != "yoda" {
		return resolver.Resolution{}, resolver.ErrMaxIDNotFound
	}
	addr, ok := f.addrs[chain.Namespace]
	if !ok {
		return resolver.Resolution{}, resolver.ErrNoDestination
	}
	a := resolver.Asset{Kind: resolver.AssetNative}
	if asset != "" && asset != "native" {
		norm, err := chains.NormalizeAddress(chain.Namespace, asset)
		if err != nil {
			return resolver.Resolution{}, resolver.ErrInvalidAsset
		}
		a = resolver.Asset{Kind: resolver.AssetToken, Address: chains.DisplayAddress(chain.Namespace, norm)}
	}
	return resolver.Resolution{
		MaxID: uuid.New(), Handle: "max.yoda", Chain: chain, Asset: a,
		Address: chains.DisplayAddress(chain.Namespace, addr), ResolutionVersion: f.version,
	}, nil
}

type fakeGateway struct {
	configured map[string]bool
	tokens     map[string]TokenInfo
	ataExists  bool
	obs        Observation
	obsErr     error
}

func (f *fakeGateway) Configured(k string) bool { return f.configured[k] }
func (f *fakeGateway) TokenInfo(_ context.Context, _ chains.Chain, addr string) (TokenInfo, error) {
	if t, ok := f.tokens[addr]; ok {
		return t, nil
	}
	return TokenInfo{}, errors.New("not a token")
}
func (f *fakeGateway) AccountExists(context.Context, chains.Chain, string) (bool, error) {
	return f.ataExists, nil
}
func (f *fakeGateway) Observe(context.Context, Payment) (Observation, error) { return f.obs, f.obsErr }

type harness struct {
	svc  *service
	repo *memRepo
	gw   *fakeGateway
	res  *fakeResolver
	prov *fakeProvider
	now  time.Time
}

func newHarness() *harness {
	repo := newMemRepo()
	res := &fakeResolver{version: 7, addrs: map[chains.Namespace]string{
		chains.NamespaceEVM: evmRecipient, chains.NamespaceSolana: solRecipient,
	}}
	gw := &fakeGateway{
		configured: map[string]bool{"base": true, "solana": true, "base-sepolia": true, "arbitrum": true},
		tokens: map[string]TokenInfo{
			usdcBase: {Decimals: 6},
			bonkMint: {Decimals: 5, TokenProgram: chains.SPLTokenProgram},
			evmOther: {Decimals: 18},
			solUSDC:  {Decimals: 6, TokenProgram: chains.SPLTokenProgram},
		},
	}
	prov := newFakeProvider()
	h := &harness{repo: repo, gw: gw, res: res, prov: prov, now: time.Now()}
	h.svc = NewService(repo, res, gw, prov, DefaultConfig(), zap.NewNop()).(*service)
	h.svc.now = func() time.Time { return h.now }
	return h
}

func prepareIn(chain, asset, amount, sender string) PrepareInput {
	return PrepareInput{Recipient: "max.yoda", Chain: chain, Asset: asset, Amount: amount, SenderAddress: sender, IdempotencyKey: uuid.NewString()}
}

func TestPrepare_Instructions(t *testing.T) {
	ctx := context.Background()

	t.Run("ETH on Base", func(t *testing.T) {
		h := newHarness()
		res, err := h.svc.Prepare(ctx, prepareIn("base", "", "0.001", evmSender))
		if err != nil {
			t.Fatal(err)
		}
		e := res.Instructions.EVM
		if e == nil || e.ChainID != 8453 || !strings.EqualFold(e.To, evmRecipient) || e.ValueHex != "0x38d7ea4c68000" || e.Data != "0x" {
			t.Fatalf("instruction %+v", e)
		}
		if res.Payment.Status != StatusAwaitingSenderSignature || res.Payment.ResolutionVersion != 7 || res.Payment.RequiredConfirmations != 2 {
			t.Fatalf("payment %+v", res.Payment)
		}
	})

	t.Run("USDC on Base calls the token contract", func(t *testing.T) {
		h := newHarness()
		res, err := h.svc.Prepare(ctx, prepareIn("base", chains.ChecksumEVMAddress(usdcBase), "25.5", evmSender))
		if err != nil {
			t.Fatal(err)
		}
		e := res.Instructions.EVM
		wantData := chains.EncodeERC20Transfer(evmRecipient, big.NewInt(25_500_000))
		if !strings.EqualFold(e.To, usdcBase) || e.ValueHex != "0x0" || e.Data != wantData {
			t.Fatalf("instruction %+v", e)
		}
		if !res.Payment.Asset.Verified || res.Payment.Asset.Symbol != "USDC" || len(res.Warnings) != 0 {
			t.Fatalf("asset %+v warnings %v", res.Payment.Asset, res.Warnings)
		}
	})

	t.Run("BONK on Solana targets the recipient ATA and warns when it must be created", func(t *testing.T) {
		h := newHarness()
		res, err := h.svc.Prepare(ctx, prepareIn("solana", bonkMint, "1000000", solSender))
		if err != nil {
			t.Fatal(err)
		}
		s := res.Instructions.Solana
		wantATA, _ := chains.AssociatedTokenAddress(solRecipient, bonkMint, chains.SPLTokenProgram)
		if s.Kind != SolanaSPLTransferChecked || s.DestinationTokenAccount != wantATA || s.Amount != "100000000000" || s.Decimals != 5 {
			t.Fatalf("instruction %+v", s)
		}
		if s.DestinationTokenAccountExists == nil || *s.DestinationTokenAccountExists || len(res.Warnings) != 1 {
			t.Fatalf("ata warning missing: %v", res.Warnings)
		}
	})

	t.Run("SOL native", func(t *testing.T) {
		h := newHarness()
		res, err := h.svc.Prepare(ctx, prepareIn("solana", "native", "0.1", solSender))
		if err != nil {
			t.Fatal(err)
		}
		if s := res.Instructions.Solana; s.Kind != SolanaSystemTransfer || s.Lamports != "100000000" || s.To != solRecipient {
			t.Fatalf("instruction %+v", s)
		}
	})

	t.Run("unverified token is allowed but flagged", func(t *testing.T) {
		h := newHarness()
		res, err := h.svc.Prepare(ctx, prepareIn("base", evmOther, "1", evmSender))
		if err != nil {
			t.Fatal(err)
		}
		if res.Payment.Asset.Verified || len(res.Warnings) == 0 {
			t.Fatal("unverified token must carry a warning")
		}
	})
}

func TestPrepare_Rejections(t *testing.T) {
	ctx := context.Background()
	v6 := int64(6)

	tests := []struct {
		name    string
		mutate  func(*harness, *PrepareInput)
		wantErr error
	}{
		{"chain without RPC", func(h *harness, in *PrepareInput) { in.Chain = "ethereum" }, ErrChainNotConfigured},
		{"sender address for wrong chain", func(h *harness, in *PrepareInput) { in.SenderAddress = solSender }, ErrInvalidSender},
		{"too many decimals for USDC", func(h *harness, in *PrepareInput) {
			in.Asset, in.Amount = usdcBase, "1.0000001"
		}, ErrInvalidAmount},
		{"zero amount", func(h *harness, in *PrepareInput) { in.Amount = "0" }, ErrInvalidAmount},
		{"recipient changed wallets since the sender reviewed", func(h *harness, in *PrepareInput) {
			in.ResolutionVersion = &v6
		}, ErrResolutionStale},
		{"sending to yourself", func(h *harness, in *PrepareInput) { in.SenderAddress = evmRecipient }, ErrSelfPayment},
		{"address that is not a token", func(h *harness, in *PrepareInput) { in.Asset = evmRecipient }, ErrAssetUnavailable},
		{"recipient has no wallet on chain", func(h *harness, in *PrepareInput) {
			delete(h.res.addrs, chains.NamespaceEVM)
		}, resolver.ErrNoDestination},
		{"verified token whose on-chain decimals disagree", func(h *harness, in *PrepareInput) {
			in.Asset = usdcBase
			h.gw.tokens[usdcBase] = TokenInfo{Decimals: 18}
		}, ErrAssetUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness()
			in := prepareIn("base", "", "1", evmSender)
			tt.mutate(h, &in)
			if _, err := h.svc.Prepare(ctx, in); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestPrepare_Idempotency(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	in := prepareIn("base", "", "0.5", evmSender)

	first, err := h.svc.Prepare(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	// Recipient reconfigures wallets; a retry must still return the original snapshot.
	h.res.version = 8
	h.res.addrs[chains.NamespaceEVM] = evmOther
	retry, err := h.svc.Prepare(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !retry.Replayed || retry.Payment.ID != first.Payment.ID || retry.Payment.RecipientAddress != evmRecipient {
		t.Fatalf("retry did not return original intent: %+v", retry.Payment)
	}

	in.Amount = "0.6"
	if _, err := h.svc.Prepare(ctx, in); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different terms with same key: %v", err)
	}
}

func TestSubmit(t *testing.T) {
	ctx := context.Background()
	owner, stranger := uuid.New(), uuid.New()

	setup := func(h *harness, caller *uuid.UUID) Payment {
		in := prepareIn("base", "", "0.5", evmSender)
		in.Caller = caller
		res, err := h.svc.Prepare(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		return res.Payment
	}

	t.Run("records the hash and is idempotent", func(t *testing.T) {
		h := newHarness()
		p := setup(h, nil)
		got, err := h.svc.Submit(ctx, SubmitInput{PaymentID: p.ID, TxHash: strings.ToUpper(evmTxHash[2:])})
		if err != nil || got.Status != StatusSubmitted || got.TxHash != evmTxHash {
			t.Fatalf("submit: %+v %v", got, err)
		}
		if again, err := h.svc.Submit(ctx, SubmitInput{PaymentID: p.ID, TxHash: evmTxHash}); err != nil || again.ID != p.ID {
			t.Fatalf("resubmit same hash: %v", err)
		}
		other := "0x" + strings.Repeat("b", 64)
		if _, err := h.svc.Submit(ctx, SubmitInput{PaymentID: p.ID, TxHash: other}); !errors.Is(err, ErrTxHashMismatch) {
			t.Fatalf("different hash: %v", err)
		}
	})

	t.Run("one transaction cannot settle two payments", func(t *testing.T) {
		h := newHarness()
		a, b := setup(h, nil), setup(h, nil)
		if _, err := h.svc.Submit(ctx, SubmitInput{PaymentID: a.ID, TxHash: evmTxHash}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.Submit(ctx, SubmitInput{PaymentID: b.ID, TxHash: evmTxHash}); !errors.Is(err, ErrTxAlreadyUsed) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("rejects malformed hashes", func(t *testing.T) {
		h := newHarness()
		p := setup(h, nil)
		for _, bad := range []string{"0x1234", solSig, "", "0x" + strings.Repeat("z", 64)} {
			if _, err := h.svc.Submit(ctx, SubmitInput{PaymentID: p.ID, TxHash: bad}); !errors.Is(err, ErrInvalidTxHash) {
				t.Errorf("%q: err = %v", bad, err)
			}
		}
	})

	t.Run("signed-in intent is hidden from other accounts", func(t *testing.T) {
		h := newHarness()
		p := setup(h, &owner)
		if _, err := h.svc.Submit(ctx, SubmitInput{Caller: &stranger, PaymentID: p.ID, TxHash: evmTxHash}); !errors.Is(err, ErrPaymentNotFound) {
			t.Fatalf("stranger: %v", err)
		}
		if _, err := h.svc.Submit(ctx, SubmitInput{PaymentID: p.ID, TxHash: evmTxHash}); !errors.Is(err, ErrPaymentNotFound) {
			t.Fatalf("anonymous: %v", err)
		}
		if _, _, err := h.svc.Get(ctx, &stranger, p.ID); !errors.Is(err, ErrPaymentNotFound) {
			t.Fatalf("get: %v", err)
		}
	})

	t.Run("late submit within grace after expiry is accepted", func(t *testing.T) {
		h := newHarness()
		p := setup(h, nil)
		h.repo.Apply(ctx, p.ID, Transition{From: StatusAwaitingSenderSignature, To: StatusExpired})
		h.now = p.ExpiresAt.Add(10 * time.Minute)
		if _, err := h.svc.Submit(ctx, SubmitInput{PaymentID: p.ID, TxHash: evmTxHash}); err != nil {
			t.Fatalf("late submit: %v", err)
		}
	})

	t.Run("submit long after expiry is refused", func(t *testing.T) {
		h := newHarness()
		p := setup(h, nil)
		h.repo.Apply(ctx, p.ID, Transition{From: StatusAwaitingSenderSignature, To: StatusExpired})
		h.now = p.ExpiresAt.Add(2 * time.Hour)
		if _, err := h.svc.Submit(ctx, SubmitInput{PaymentID: p.ID, TxHash: evmTxHash}); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestProcess(t *testing.T) {
	ctx := context.Background()

	submitted := func(h *harness, chain, sender, hash string) Payment {
		res, err := h.svc.Prepare(ctx, prepareIn(chain, "", "0.5", sender))
		if err != nil {
			t.Fatal(err)
		}
		p, err := h.svc.Submit(ctx, SubmitInput{PaymentID: res.Payment.ID, TxHash: hash})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	tests := []struct {
		name       string
		chain      string
		obs        Observation
		obsErr     error
		startIn    Status
		age        time.Duration
		wantStatus Status
	}{
		{"pending stays submitted", "base", Observation{}, nil, StatusSubmitted, time.Minute, StatusSubmitted},
		{"seen but shallow → confirming", "base", Observation{Found: true, Confirmations: 1}, nil, StatusSubmitted, 0, StatusConfirming},
		{"deep enough → completed", "base", Observation{Found: true, Confirmations: 2, Final: true}, nil, StatusSubmitted, 0, StatusCompleted},
		{"confirming → completed", "base", Observation{Found: true, Confirmations: 2, Final: true}, nil, StatusConfirming, 0, StatusCompleted},
		{"reverted → failed", "base", Observation{Found: true, Reverted: true}, nil, StatusSubmitted, 0, StatusFailed},
		{"wrong transfer → failed", "base", Observation{Found: true, Mismatch: "nope"}, nil, StatusSubmitted, 0, StatusFailed},
		{"reorg drops it → back to submitted", "base", Observation{}, nil, StatusConfirming, 0, StatusSubmitted},
		{"EVM unseen after timeout → manual review", "base", Observation{}, nil, StatusSubmitted, time.Hour, StatusManualReview},
		{"Solana unseen after blockhash expiry → failed", "solana", Observation{}, nil, StatusSubmitted, 10 * time.Minute, StatusFailed},
		{"RPC error leaves state untouched", "base", Observation{}, errors.New("rpc down"), StatusSubmitted, time.Hour, StatusSubmitted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness()
			sender, hash := evmSender, evmTxHash
			if tt.chain == "solana" {
				sender, hash = solSender, solSig
			}
			p := submitted(h, tt.chain, sender, hash)
			if tt.startIn == StatusConfirming {
				p, _ = h.repo.Apply(ctx, p.ID, Transition{From: StatusSubmitted, To: StatusConfirming})
			}
			h.gw.obs, h.gw.obsErr = tt.obs, tt.obsErr
			h.now = p.SubmittedAt.Add(tt.age)

			if err := h.svc.Process(ctx, p); err != nil {
				t.Fatal(err)
			}
			got, _ := h.repo.Get(ctx, p.ID)
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %s (%s), want %s", got.Status, got.StatusReason, tt.wantStatus)
			}
			if tt.wantStatus == StatusCompleted && got.CompletedAt == nil {
				t.Error("completed_at not set")
			}
		})
	}
}

func TestProcess_ConcurrentWorkersApplyOnce(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	res, _ := h.svc.Prepare(ctx, prepareIn("base", "", "0.5", evmSender))
	p, _ := h.svc.Submit(ctx, SubmitInput{PaymentID: res.Payment.ID, TxHash: evmTxHash})
	h.gw.obs = Observation{Found: true, Confirmations: 5, Final: true}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.svc.Process(ctx, p)
		}()
	}
	wg.Wait()

	completed := 0
	events, _ := h.repo.Events(ctx, p.ID)
	for _, e := range events {
		if e.ToStatus == StatusCompleted {
			completed++
		}
	}
	if completed != 1 {
		t.Fatalf("completed recorded %d times, want 1", completed)
	}
}

func TestPrepare_ReceiveOnlyChain(t *testing.T) {
	ctx := context.Background()
	const btcRecipient = "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"
	const tronRecipient = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	withReceiveOnly := func() *harness {
		h := newHarness()
		h.res.addrs[chains.NamespaceBitcoin] = btcRecipient
		h.res.addrs[chains.NamespaceTron] = tronRecipient
		h.prov.assets["bitcoin|native|"] = routing.ProviderAsset{ID: "nep141:btc.omft.near", Decimals: 8, Symbol: "BTC"}
		h.prov.assets["tron|native|"] = routing.ProviderAsset{ID: "nep141:tron.omft.near", Decimals: 6, Symbol: "TRX"}
		return h
	}

	t.Run("paying on a receive-only chain is refused", func(t *testing.T) {
		_, err := withReceiveOnly().svc.Prepare(ctx, prepareIn("bitcoin", "", "1", evmSender))
		var ae *shared.AppError
		if !errors.Is(err, ErrReceiveOnlyChain) || !errors.As(err, &ae) || ae.Message != "MAX can't send on Bitcoin yet." {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("SOL in, BTC out to the pasted address", func(t *testing.T) {
		h := withReceiveOnly()
		res, err := h.svc.Prepare(ctx, crossIn("solana", "native", "1", solSender, "bitcoin", "native"))
		if err != nil {
			t.Fatal(err)
		}
		if res.Payment.Route != RouteCrossChain || res.Payment.RecipientAddress != btcRecipient {
			t.Fatalf("payment %+v", res.Payment)
		}
		if q := h.prov.lastQuote; q.Recipient != btcRecipient || q.Destination.Chain.Key != "bitcoin" || q.Destination.Kind != routing.AssetNative {
			t.Fatalf("quote request %+v", q)
		}
		if !strings.Contains(strings.Join(res.Warnings, " "), "BTC on Bitcoin") {
			t.Fatalf("missing disclosure: %v", res.Warnings)
		}
	})

	t.Run("ETH on Base, TRX out on Tron", func(t *testing.T) {
		h := withReceiveOnly()
		if _, err := h.svc.Prepare(ctx, crossIn("base", "native", "1", evmSender, "tron", "native")); err != nil {
			t.Fatal(err)
		}
		if h.prov.lastQuote.Recipient != tronRecipient {
			t.Fatalf("quote request %+v", h.prov.lastQuote)
		}
	})

	t.Run("auto-convert to BTC when the recipient has no wallet on the paying chain", func(t *testing.T) {
		h := withReceiveOnly()
		delete(h.res.addrs, chains.NamespaceSolana)
		btc, _ := chains.Lookup("bitcoin")
		h.res.settlement = &resolver.Settlement{Chain: btc, Asset: resolver.Asset{Kind: resolver.AssetNative}}
		res, err := h.svc.Prepare(ctx, prepareIn("solana", "native", "1", solSender))
		if err != nil {
			t.Fatal(err)
		}
		if res.Payment.Route != RouteCrossChain || h.prov.lastQuote.Recipient != btcRecipient {
			t.Fatalf("payment %+v quote %+v", res.Payment, h.prov.lastQuote)
		}
	})
}
