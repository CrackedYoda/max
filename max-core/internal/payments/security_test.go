package payments

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Regressions for the 2026-11-10 security review (S04, S10, S11).

func TestReplay_RespectsOwner(t *testing.T) {
	ctx := context.Background()
	owner, stranger := uuid.New(), uuid.New()

	h := newHarness()
	in := prepareIn("base", "", "0.001", evmSender)
	in.Caller = &owner
	first, err := h.svc.Prepare(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := h.svc.Prepare(ctx, in); err != nil || again.Payment.ID != first.Payment.ID {
		t.Fatalf("owner replay: %v", err)
	}
	for name, caller := range map[string]*uuid.UUID{"anonymous": nil, "other account": &stranger} {
		in.Caller = caller
		if _, err := h.svc.Prepare(ctx, in); !errors.Is(err, ErrIdempotencyConflict) {
			t.Errorf("%s replay: err = %v, want conflict", name, err)
		}
	}

	// An anonymous intent stays replayable anonymously, but not by an account.
	anon := prepareIn("base", "", "0.001", evmSender)
	if _, err := h.svc.Prepare(ctx, anon); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Prepare(ctx, anon); err != nil {
		t.Fatalf("anonymous replay: %v", err)
	}
	anon.Caller = &owner
	if _, err := h.svc.Prepare(ctx, anon); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("account replaying anonymous intent: %v", err)
	}
}

func TestReplay_ComparesCrossChainTerms(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	in := crossIn("base", "native", "1", evmSender, "solana", solUSDC)
	if _, err := h.svc.Prepare(ctx, in); err != nil {
		t.Fatal(err)
	}

	native := in
	native.DestinationAsset = "native"
	if _, err := h.svc.Prepare(ctx, native); !errors.Is(err, ErrIdempotencyConflict) {
		t.Errorf("different destination asset: %v", err)
	}
	slip := in
	bps := DefaultConfig().DefaultSlippageBps + 50
	slip.SlippageBps = &bps
	if _, err := h.svc.Prepare(ctx, slip); !errors.Is(err, ErrIdempotencyConflict) {
		t.Errorf("different slippage: %v", err)
	}
	stale := in
	v := int64(99)
	stale.ResolutionVersion = &v
	if _, err := h.svc.Prepare(ctx, stale); !errors.Is(err, ErrResolutionStale) {
		t.Errorf("new resolution version: %v", err)
	}
	if again, err := h.svc.Prepare(ctx, in); err != nil || !again.Replayed {
		t.Errorf("identical retry: %v", err)
	}
}

func TestSubmit_DeadlineDoesNotDependOnSweeper(t *testing.T) {
	ctx := context.Background()
	grace := DefaultConfig().LateSubmitGrace
	for _, tc := range []struct {
		name  string
		after time.Duration
		ok    bool
	}{
		{"before expiry", -time.Minute, true},
		{"within grace, unswept", grace / 2, true},
		{"after grace, unswept", grace + time.Minute, false},
		{"a day later, unswept", 24 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			p, err := h.svc.Prepare(ctx, prepareIn("base", "", "0.001", evmSender))
			if err != nil {
				t.Fatal(err)
			}
			h.now = p.Payment.ExpiresAt.Add(tc.after)
			_, err = h.svc.Submit(ctx, SubmitInput{PaymentID: p.Payment.ID, TxHash: evmTxHash})
			if tc.ok && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidState) {
				t.Fatalf("err = %v, want invalid state", err)
			}
		})
	}

	// A cross-chain intent stops at the quote deadline even before the sweeper runs.
	h := newHarness()
	p, err := h.svc.Prepare(ctx, crossIn("base", "native", "1", evmSender, "solana", solUSDC))
	if err != nil {
		t.Fatal(err)
	}
	h.now = p.Payment.CrossChain.QuoteDeadline.Add(time.Second)
	if _, err := h.svc.Submit(ctx, SubmitInput{PaymentID: p.Payment.ID, TxHash: evmTxHash}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("after quote deadline: %v", err)
	}
}
