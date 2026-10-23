//go:build live

package nearintents

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/max/max-core/internal/routing"
)

// Run with: go test -tags live -run Live ./internal/routing/nearintents/
// Requests a real (uncommitted) quote; no funds move unless someone deposits.
func TestLive_QuoteBaseETHToSolanaUSDC(t *testing.T) {
	ctx := context.Background()
	c := New(Config{Referral: "max"})
	req := routing.QuoteRequest{
		Origin:      routing.AssetRef{Chain: chain("base"), Kind: routing.AssetNative},
		Destination: routing.AssetRef{Chain: chain("solana"), Kind: routing.AssetToken, Address: solUSDC},
		SwapType:    routing.ExactInput,
		Amount:      big.NewInt(10_000_000_000_000_000), // 0.01 ETH
		Recipient:   solRecip,
		RefundTo:    "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		SlippageBps: 100,
		Deadline:    time.Now().Add(45 * time.Minute),
	}
	q, err := c.Quote(ctx, req)
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	t.Logf("deposit=%s in=%s out=%s min=%s usdIn=%s eta=%s deadline=%s",
		q.DepositAddress, q.AmountIn, q.AmountOut, q.MinAmountOut, q.AmountInUSD, q.TimeEstimate, q.Deadline.Format(time.RFC3339))
	if err := routing.ValidateQuote(req, q, time.Now(), time.Now().Add(25*time.Minute)); err != nil {
		t.Fatalf("live quote fails MAX validation: %v", err)
	}

	st, err := c.Status(ctx, q.DepositAddress)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	t.Logf("status of fresh deposit address: %s", st.State)
}
