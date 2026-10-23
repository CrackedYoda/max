package renames

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
)

const treasurySol = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"

type fakeRepo struct {
	ids      map[uuid.UUID]*MaxIDInfo
	owner    uuid.UUID
	requests map[uuid.UUID]*Request
	taken    map[string]bool
	history  []HistoryEntry
}

func (f *fakeRepo) GetOwnedMaxID(_ context.Context, id, owner uuid.UUID) (MaxIDInfo, error) {
	m, ok := f.ids[id]
	if !ok || owner != f.owner {
		return MaxIDInfo{}, maxids.ErrMaxIDNotFound
	}
	return *m, nil
}

func (f *fakeRepo) GetReserved(_ context.Context, n string) (*maxids.Reserved, error) {
	if n == "admin" {
		return &maxids.Reserved{CanonicalName: n, Policy: maxids.PolicyBlocked}, nil
	}
	return nil, nil
}

func (f *fakeRepo) Reserve(_ context.Context, p ReserveParams) (Request, error) {
	if f.taken[p.NewName] {
		return Request{}, maxids.ErrNameTaken
	}
	for _, r := range f.requests {
		if r.MaxID == p.MaxID && r.Status == StatusAwaitingPayment {
			return Request{}, ErrRenamePending
		}
	}
	r := &Request{ID: uuid.New(), MaxID: p.MaxID, OwnerID: p.Owner, OldName: p.OldName, NewName: p.NewName,
		Status: StatusAwaitingPayment, FeePaymentID: p.FeePaymentID}
	f.requests[r.ID] = r
	f.taken[p.NewName] = true
	return *r, nil
}

func (f *fakeRepo) Get(_ context.Context, id, owner uuid.UUID) (Request, error) {
	r, ok := f.requests[id]
	if !ok || r.OwnerID != owner {
		return Request{}, ErrRequestNotFound
	}
	return *r, nil
}

func (f *fakeRepo) GetPending(_ context.Context, maxID uuid.UUID) (*Request, error) {
	for _, r := range f.requests {
		if r.MaxID == maxID && r.Status == StatusAwaitingPayment {
			c := *r
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) ListPending(context.Context, int32) ([]Request, error) {
	var out []Request
	for _, r := range f.requests {
		if r.Status == StatusAwaitingPayment {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *fakeRepo) Finalize(_ context.Context, id uuid.UUID, cooldown, _ time.Duration) (Request, error) {
	r := f.requests[id]
	if r.Status != StatusAwaitingPayment {
		return *r, nil
	}
	m := f.ids[r.MaxID]
	f.history = append(f.history, HistoryEntry{OldName: m.Name, NewName: r.NewName})
	m.Name, m.RenameAvailableAt = r.NewName, time.Now().Add(cooldown)
	r.Status = StatusCompleted
	return *r, nil
}

func (f *fakeRepo) Close(_ context.Context, id uuid.UUID, st Status, reason string) (Request, error) {
	r := f.requests[id]
	if r.Status != StatusAwaitingPayment {
		return Request{}, ErrRequestNotFound
	}
	r.Status, r.FailureReason = st, reason
	delete(f.taken, r.NewName)
	return *r, nil
}

func (f *fakeRepo) History(context.Context, uuid.UUID) ([]HistoryEntry, error) { return f.history, nil }

type fakePayments struct {
	byID     map[uuid.UUID]payments.Payment
	lastFee  payments.FeeInput
	rejected []uuid.UUID
}

func (f *fakePayments) PrepareFee(_ context.Context, in payments.FeeInput) (payments.Prepared, error) {
	f.lastFee = in
	status := payments.StatusAwaitingSenderSignature
	if in.Hold {
		status = payments.StatusCreated
	}
	p := payments.Payment{ID: uuid.New(), Purpose: in.Purpose, Status: status, Chain: in.Chain, Amount: in.Amount,
		RecipientAddress: in.Treasury, ExpiresAt: time.Now().Add(15 * time.Minute)}
	f.byID[p.ID] = p
	return payments.Prepared{Payment: p}, nil
}

func (f *fakePayments) Get(_ context.Context, _ *uuid.UUID, id uuid.UUID) (payments.Payment, []payments.Event, error) {
	return f.byID[id], nil, nil
}

func (f *fakePayments) Release(_ context.Context, id uuid.UUID, _ string) (payments.Payment, error) {
	p := f.byID[id]
	p.Status = payments.StatusAwaitingSenderSignature
	f.byID[id] = p
	return p, nil
}

func (f *fakePayments) Reject(_ context.Context, id uuid.UUID, _ string) (payments.Payment, error) {
	f.rejected = append(f.rejected, id)
	p := f.byID[id]
	p.Status = payments.StatusFailed
	f.byID[id] = p
	return p, nil
}

func (f *fakePayments) Instructions(payments.Payment) payments.Instructions {
	return payments.Instructions{Solana: &payments.SolanaInstruction{To: treasurySol}}
}

type harness struct {
	svc   *service
	repo  *fakeRepo
	pay   *fakePayments
	owner uuid.UUID
	maxID uuid.UUID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fees, err := ParseFees("solana:native:0.05")
	if err != nil {
		t.Fatal(err)
	}
	owner, maxID := uuid.New(), uuid.New()
	repo := &fakeRepo{owner: owner, requests: map[uuid.UUID]*Request{}, taken: map[string]bool{"yoda": true},
		ids: map[uuid.UUID]*MaxIDInfo{maxID: {ID: maxID, Name: "yoda", Status: "active", RenameAvailableAt: time.Now().Add(-time.Minute)}}}
	pay := &fakePayments{byID: map[uuid.UUID]payments.Payment{}}
	svc := NewService(repo, pay, Config{
		Quarantine: 30 * 24 * time.Hour, Fees: fees, LateSubmitGrace: time.Hour,
		Treasury: map[chains.Namespace]string{chains.NamespaceSolana: treasurySol},
	}, zap.NewNop()).(*service)
	return &harness{svc: svc, repo: repo, pay: pay, owner: owner, maxID: maxID}
}

func (h *harness) request(name string) (RequestView, error) {
	return h.svc.Request(context.Background(), h.owner, h.maxID, RequestInput{
		NewName: name, FeeChain: "solana", FeeAsset: "native",
		SenderAddress: "4Nd1mBQtrMJVYVfKf2PJy9NZUZdTAsp7D4xWLs4gDB4T", IdempotencyKey: uuid.NewString(),
	})
}

func TestRequest(t *testing.T) {
	h := newHarness(t)
	v, err := h.request("MAX.Jedi")
	if err != nil {
		t.Fatal(err)
	}
	if v.Request.NewName != "jedi" || v.Request.Status != StatusAwaitingPayment || v.Instructions == nil {
		t.Fatalf("view %+v", v)
	}
	fee := h.pay.lastFee
	if !fee.Hold || fee.Purpose != payments.PurposeRenameFee || fee.Treasury != treasurySol || fee.Amount.Cmp(big.NewInt(50_000_000)) != 0 {
		t.Fatalf("fee intent %+v", fee)
	}
	if v.Payment.Status != payments.StatusAwaitingSenderSignature {
		t.Fatal("fee intent must be released only after the name is reserved")
	}
	if !strings.Contains(fee.Label, "max.yoda → max.jedi") {
		t.Fatalf("label %q", fee.Label)
	}
}

func TestRequest_Rejections(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*harness)
		newName  string
		feeChain string
		wantErr  error
		wantFee  bool // whether a fee intent was created (and must then be rejected)
	}{
		{"cooldown", func(h *harness) { h.repo.ids[h.maxID].RenameAvailableAt = time.Now().Add(time.Hour) }, "jedi", "solana", ErrCooldown, false},
		{"same name", nil, "YODA", "solana", ErrSameName, false},
		{"reserved name", nil, "admin", "solana", maxids.ErrNameReserved, false},
		{"invalid name", nil, "x", "solana", maxids.ErrInvalidName, false},
		{"unaccepted fee chain", nil, "jedi", "base", ErrFeeOption, false},
		{"disabled", func(h *harness) { h.svc.cfg.Fees = nil }, "jedi", "solana", ErrRenamesDisabled, false},
		{"inactive ID", func(h *harness) { h.repo.ids[h.maxID].Status = "locked" }, "jedi", "solana", maxids.ErrMaxIDNotActive, false},
		{"name taken at reservation time", func(h *harness) { h.repo.taken["jedi"] = true }, "jedi", "solana", maxids.ErrNameTaken, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			if tt.setup != nil {
				tt.setup(h)
			}
			_, err := h.svc.Request(context.Background(), h.owner, h.maxID, RequestInput{
				NewName: tt.newName, FeeChain: tt.feeChain, SenderAddress: "4Nd1mBQtrMJVYVfKf2PJy9NZUZdTAsp7D4xWLs4gDB4T", IdempotencyKey: uuid.NewString(),
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			created := len(h.pay.byID) > 0
			if created != tt.wantFee {
				t.Fatalf("fee intent created = %v, want %v", created, tt.wantFee)
			}
			if created && len(h.pay.rejected) != 1 {
				t.Fatal("an unreservable rename must fail its held fee intent so it can never be paid")
			}
		})
	}
}

func TestReconcile(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name         string
		feeStatus    payments.Status
		expiredAgo   time.Duration
		wantStatus   Status
		wantRenamed  bool
		wantCooldown bool
	}{
		{"fee completed → renamed, cooldown starts", payments.StatusCompleted, 0, StatusCompleted, true, true},
		{"fee failed → released, no cooldown", payments.StatusFailed, 0, StatusFailed, false, false},
		{"fee refunded → released", payments.StatusRefunded, 0, StatusFailed, false, false},
		{"fee in flight → still pending", payments.StatusConfirming, 0, StatusAwaitingPayment, false, false},
		{"expired but inside late-submit grace → still pending", payments.StatusExpired, 10 * time.Minute, StatusAwaitingPayment, false, false},
		{"expired past grace → released", payments.StatusExpired, 2 * time.Hour, StatusExpired, false, false},
		{"manual review → held for a human", payments.StatusManualReview, 0, StatusAwaitingPayment, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			v, err := h.request("jedi")
			if err != nil {
				t.Fatal(err)
			}
			p := h.pay.byID[v.Payment.ID]
			p.Status = tt.feeStatus
			p.ExpiresAt = time.Now().Add(-tt.expiredAgo)
			h.pay.byID[p.ID] = p

			if _, err := h.svc.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
			got, _ := h.svc.Get(ctx, h.owner, h.maxID, v.Request.ID)
			if got.Request.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", got.Request.Status, tt.wantStatus)
			}
			m := h.repo.ids[h.maxID]
			if renamed := m.Name == "jedi"; renamed != tt.wantRenamed {
				t.Fatalf("renamed = %v", renamed)
			}
			if cooling := time.Now().Before(m.RenameAvailableAt); cooling != tt.wantCooldown {
				t.Fatalf("cooldown active = %v, want %v", cooling, tt.wantCooldown)
			}
			if tt.wantStatus != StatusAwaitingPayment && !tt.wantRenamed && h.repo.taken["jedi"] {
				t.Fatal("a released reservation must free the name")
			}
		})
	}
}

func TestOnePendingRenameAtATime(t *testing.T) {
	h := newHarness(t)
	if _, err := h.request("jedi"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.request("sith"); !errors.Is(err, ErrRenamePending) {
		t.Fatalf("err = %v", err)
	}
	e, _ := h.svc.Eligibility(context.Background(), h.owner, h.maxID)
	if e.Eligible || e.Pending == nil {
		t.Fatalf("eligibility %+v", e)
	}
}

func TestParseFees(t *testing.T) {
	fees, err := ParseFees("solana:native:0.05, base:0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913:5")
	if err != nil {
		t.Fatal(err)
	}
	if len(fees) != 2 || fees[0].Amount.String() != "50000000" || fees[1].Amount.String() != "5000000" || fees[1].Symbol != "USDC" {
		t.Fatalf("fees %+v", fees)
	}
	for _, bad := range []string{
		"solana:native", "tron:native:1", "base:0x1111111111111111111111111111111111111111:1",
		"solana:native:0", "solana:native:0.0000000001",
	} {
		if _, err := ParseFees(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := ParseTreasury("", "", fees); err == nil {
		t.Error("fees without treasury accepted")
	}
}
