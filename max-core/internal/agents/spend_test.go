package agents

import (
	"math/big"
	"testing"

	"github.com/max/max-core/db/generated"
)

// security-review S02: an untrusted payments:submit caller could attach an
// unrelated hash, get the payment marked FAILED, and so release the budget
// while the real authorized transaction still settles. Only outcomes that
// prove the funds never moved (or came back) may release a reservation.
func TestReservationHeldUntilOutcomeIsConclusive(t *testing.T) {
	tests := []struct {
		status, failure string
		held            bool
	}{
		{"AWAITING_SENDER_SIGNATURE", "", true},
		{"SUBMITTED", "", true},
		{"COMPLETED", "", true},
		{"MANUAL_REVIEW", "", true},
		{"REFUND_PENDING", "", true},
		// An unrelated or mismatching hash proves nothing about the real payment.
		{"FAILED", "mismatch", true},
		// The hash was never seen: it may be fabricated while the real one lands.
		{"FAILED", "not_found", true},
		// Approved, then expired without a recorded hash: it may have been broadcast.
		{"EXPIRED", "", true},
		// The payment's own transfer failed on-chain (the gateway checks it is
		// the payment's transfer, from the agent's wallet).
		{"FAILED", "reverted", false},
		// Rejected before it was ever signable.
		{"FAILED", "rejected", false},
		{"REFUNDED", "", false},
	}
	for _, tt := range tests {
		if got := reservationHeld(tt.status, tt.failure); got != tt.held {
			t.Errorf("reservationHeld(%s, %q) = %v, want %v", tt.status, tt.failure, got, tt.held)
		}
	}
}

func TestSpendCountsHeldReservations(t *testing.T) {
	rows := []generated.ListAgentSpend24hRow{
		{Chain: "solana", AssetAddress: "native", AmountBaseUnits: "100", PaymentStatus: "COMPLETED"},
		{Chain: "solana", AssetAddress: "native", AmountBaseUnits: "200", PaymentStatus: "FAILED", FailureEvent: "mismatch"},
		{Chain: "solana", AssetAddress: "native", AmountBaseUnits: "400", PaymentStatus: "EXPIRED"},
		{Chain: "solana", AssetAddress: "native", AmountBaseUnits: "800", PaymentStatus: "FAILED", FailureEvent: "reverted"},
		// Market rows carry no payment status; their own filter already applied.
		{Chain: "solana", AssetAddress: "native", AmountBaseUnits: "1000"},
	}
	spent, err := sumSpend(rows)
	if err != nil {
		t.Fatal(err)
	}
	if got := spent("solana", "native"); got.Cmp(big.NewInt(1700)) != 0 {
		t.Fatalf("spent = %s, want 1700 (the reverted 800 is released)", got)
	}
}
