package routing

import (
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestValidateQuote(t *testing.T) {
	now := time.Now()
	live := now.Add(15 * time.Minute)
	good := func() Quote {
		return Quote{
			DepositAddress: "0xdeposit", Signature: "sig",
			AmountIn: big.NewInt(1_000_000), AmountOut: big.NewInt(500_000), MinAmountOut: big.NewInt(495_000),
			Deadline: now.Add(30 * time.Minute),
		}
	}
	in := QuoteRequest{SwapType: ExactInput, Amount: big.NewInt(1_000_000), SlippageBps: 100}
	out := QuoteRequest{SwapType: ExactOutput, Amount: big.NewInt(500_000), SlippageBps: 100}

	tests := []struct {
		name    string
		req     QuoteRequest
		mutate  func(*Quote)
		wantErr string
	}{
		{"exact input ok", in, nil, ""},
		{"exact input amount changed by provider", in, func(q *Quote) { q.AmountIn = big.NewInt(1_000_001) }, "differs from requested"},
		{"slippage wider than agreed", in, func(q *Quote) { q.MinAmountOut = big.NewInt(494_999) }, "slippage floor"},
		{"min above expected", in, func(q *Quote) { q.MinAmountOut = big.NewInt(600_000) }, "exceeds expected"},
		{"no deposit address", in, func(q *Quote) { q.DepositAddress = "" }, "no deposit address"},
		{"unsigned", in, func(q *Quote) { q.Signature = "" }, "unsigned"},
		{"zero output", in, func(q *Quote) { q.AmountOut = big.NewInt(0) }, "must be positive"},
		{"deadline before intent expiry", in, func(q *Quote) { q.Deadline = now.Add(10 * time.Minute) }, "too soon"},
		{"exact output ok", out, func(q *Quote) { q.MinAmountOut = big.NewInt(500_000) }, ""},
		{"exact output short", out, func(q *Quote) { q.AmountOut = big.NewInt(499_999); q.MinAmountOut = big.NewInt(499_999) }, "below requested"},
		{"exact output min below target", out, nil, "below the exact amount"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := good()
			if tt.mutate != nil {
				tt.mutate(&q)
			}
			err := ValidateQuote(tt.req, q, now, live)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
