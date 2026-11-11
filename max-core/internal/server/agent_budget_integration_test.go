//go:build integration

package server_test

import (
	"context"
	"net/http"
	"testing"
)

// security-review S02: a FAILED payment releases its budget reservation only
// when the payment's own transfer failed ("reverted"). An unrelated hash that
// fails as a mismatch, or an approved intent that expired, keeps counting.
func TestAgentBudget_HeldUntilOutcomeIsConclusive(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	alice := &client{t: t, h: h}
	alice.prove(newSol(), "login")
	alice.do("POST", "/v1/ids", map[string]any{"name": "yoda"})
	dev := &client{t: t, h: h}
	dev.prove(newSol(), "login")
	dev.do("POST", "/v1/ids", map[string]any{"name": "devdude"})

	_, wallet, agent := setupAgent(t, h, dev, "budgetbot", map[string]any{
		"limits":            []map[string]any{{"chain": "solana", "asset": "native", "maxPerTransfer": "0.2", "maxPer24h": "0.5"}},
		"allowedRecipients": []string{"max.yoda"},
	}, []string{"payments:propose", "payments:read", "payments:submit", "authorizations:read"})

	proposeDecision := func(key string) (string, string) {
		t.Helper()
		code, body := propose(t, agent, wallet, "max.yoda", "0.2", key)
		if code != http.StatusCreated && code != http.StatusOK {
			t.Fatalf("propose %s: %d %v", key, code, body)
		}
		got := data(t, body)
		return got["payment"].(map[string]any)["id"].(string), got["authorization"].(map[string]any)["decision"].(string)
	}
	fail := func(paymentID, event string) {
		t.Helper()
		ctx := context.Background()
		if _, err := env.pool.Exec(ctx, `UPDATE payments SET status = 'FAILED' WHERE id = $1`, paymentID); err != nil {
			t.Fatal(err)
		}
		if _, err := env.pool.Exec(ctx, `INSERT INTO payment_events (payment_id, event_type, from_status, to_status)
			VALUES ($1, $2, 'SUBMITTED', 'FAILED')`, paymentID, event); err != nil {
			t.Fatal(err)
		}
	}

	first, d := proposeDecision("budget-0000001")
	if d != "auto_approved" {
		t.Fatalf("first: %s", d)
	}
	// An unrelated hash failed as a mismatch: the real 0.2 may still settle.
	fail(first, "mismatch")

	second, d := proposeDecision("budget-0000002")
	if d != "auto_approved" {
		t.Fatalf("second: %s", d)
	}
	// 0.2 (held) + 0.2 + 0.2 > 0.5: before the fix the mismatch released the
	// first reservation and this was auto-approved.
	if _, d = proposeDecision("budget-0000003"); d != "pending_approval" {
		t.Fatalf("third with a held mismatch: %s, want pending_approval", d)
	}

	// The second payment's own transfer failed on-chain: released.
	fail(second, "reverted")
	if _, d = proposeDecision("budget-0000004"); d != "auto_approved" {
		t.Fatalf("after a conclusive revert: %s, want auto_approved", d)
	}
}
