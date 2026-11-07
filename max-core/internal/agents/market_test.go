package agents

import (
	"math/big"
	"strings"
	"testing"
)

const usdcMint = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"

func usdc(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000)) }

func marketPolicy() Policy {
	return Policy{
		Limits: []AssetLimit{{Chain: "solana-devnet", Asset: usdcMint, MaxPerTransfer: usdc(50), MaxPer24h: usdc(100)}},
		Market: MarketPolicy{CanSell: true, MinSalePrices: []SaleMinimum{{Chain: "solana-devnet", Asset: usdcMint, Amount: usdc(20).String()}}},
	}
}

func TestEvaluateMarket_BuyWithinLimit(t *testing.T) {
	for _, action := range []string{MarketActionBuy, MarketActionOffer, MarketActionPay} {
		ev := EvaluateMarket(marketPolicy(), StatusActive, action, usdc(40), "solana-devnet", usdcMint, usdc(60))
		if ev.Outcome != DecisionAutoApproved || len(ev.Reasons) != 0 {
			t.Fatalf("%s within limits: %+v", action, ev)
		}
	}
	// Recipient allowlists are for transfers; a market buy has no recipient.
	p := marketPolicy()
	p.AllowedRecipients = []string{"yoda"}
	if ev := EvaluateMarket(p, StatusActive, MarketActionBuy, usdc(1), "solana-devnet", usdcMint, nil); ev.Outcome != DecisionAutoApproved {
		t.Fatalf("allowlist applied to buy: %+v", ev)
	}
}

func TestEvaluateMarket_BuyOverLimitPending(t *testing.T) {
	tests := []struct {
		name          string
		amount, spent *big.Int
		chain, asset  string
		reason        string
	}{
		{"per transfer", usdc(51), usdc(0), "solana-devnet", usdcMint, "per-transfer"},
		{"rolling 24h", usdc(41), usdc(60), "solana-devnet", usdcMint, "24h spend"},
		{"no limit for asset", usdc(1), nil, "solana-devnet", "", "no delegated limit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []string{MarketActionBuy, MarketActionPay} {
				ev := EvaluateMarket(marketPolicy(), StatusActive, action, tc.amount, tc.chain, tc.asset, tc.spent)
				if ev.Outcome != DecisionPendingApproval || !strings.Contains(strings.Join(ev.Reasons, ";"), tc.reason) {
					t.Fatalf("%s: %+v", action, ev)
				}
			}
		})
	}
	if ev := EvaluateMarket(marketPolicy(), StatusPaused, MarketActionBuy, usdc(1), "solana-devnet", usdcMint, nil); ev.Outcome != DecisionDenied {
		t.Fatalf("paused agent: %+v", ev)
	}
	if ev := EvaluateMarket(marketPolicy(), StatusActive, "steal", usdc(1), "solana-devnet", usdcMint, nil); ev.Outcome != DecisionDenied {
		t.Fatalf("unknown action: %+v", ev)
	}
}

func TestEvaluateMarket_SellNeedsCanSell(t *testing.T) {
	p := marketPolicy()
	p.Market.CanSell = false
	for _, action := range []string{MarketActionList, MarketActionAccept} {
		ev := EvaluateMarket(p, StatusActive, action, usdc(30), "solana-devnet", usdcMint, nil)
		if ev.Outcome != DecisionPendingApproval || !strings.Contains(strings.Join(ev.Reasons, ";"), "not allowed to sell") {
			t.Fatalf("%s without canSell: %+v", action, ev)
		}
		if ev := EvaluateMarket(marketPolicy(), StatusActive, action, usdc(30), "solana-devnet", usdcMint, nil); ev.Outcome != DecisionAutoApproved {
			t.Fatalf("%s with canSell: %+v", action, ev)
		}
	}
	// Selling is not spending: per-transfer and 24h limits do not apply.
	if ev := EvaluateMarket(marketPolicy(), StatusActive, MarketActionList, usdc(1000), "solana-devnet", usdcMint, usdc(100)); ev.Outcome != DecisionAutoApproved {
		t.Fatalf("sale above spend limit: %+v", ev)
	}
}

func TestEvaluateMarket_SellBelowMinPending(t *testing.T) {
	for _, action := range []string{MarketActionList, MarketActionAccept} {
		ev := EvaluateMarket(marketPolicy(), StatusActive, action, new(big.Int).Sub(usdc(20), big.NewInt(1)), "solana-devnet", usdcMint, nil)
		if ev.Outcome != DecisionPendingApproval || !strings.Contains(strings.Join(ev.Reasons, ";"), "below the minimum sale price") {
			t.Fatalf("%s below min: %+v", action, ev)
		}
		if ev := EvaluateMarket(marketPolicy(), StatusActive, action, usdc(20), "solana-devnet", usdcMint, nil); ev.Outcome != DecisionAutoApproved {
			t.Fatalf("%s at min: %+v", action, ev)
		}
	}
}

func TestEvaluateMarket_SellNoMinimumForAssetPending(t *testing.T) {
	for _, action := range []string{MarketActionList, MarketActionAccept} {
		ev := EvaluateMarket(marketPolicy(), StatusActive, action, usdc(30), "solana", "", nil)
		if ev.Outcome != DecisionPendingApproval || !strings.Contains(strings.Join(ev.Reasons, ";"), "no minimum sale price") {
			t.Fatalf("%s in an asset without a minimum: %+v", action, ev)
		}
	}
}

func TestEvaluateMarket_MinMisconfiguredFailsClosed(t *testing.T) {
	for _, bad := range []string{"", "abc", "-1", "1.5"} {
		p := marketPolicy()
		p.Market.MinSalePrices[0].Amount = bad
		ev := EvaluateMarket(p, StatusActive, MarketActionList, usdc(1000), "solana-devnet", usdcMint, nil)
		if ev.Outcome != DecisionPendingApproval || !strings.Contains(strings.Join(ev.Reasons, ";"), "minimum sale price misconfigured") {
			t.Fatalf("minimum %q: %+v", bad, ev)
		}
	}
}

func TestBuildMarketPolicy(t *testing.T) {
	m, err := buildMarketPolicy(MarketPolicyInput{CanSell: true, MinSalePrices: []SaleMinimumInput{
		{Chain: "solana-devnet", Asset: usdcMint, Amount: "20000000"},
		{Chain: "solana", Asset: "native", Amount: "0"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !m.CanSell || len(m.MinSalePrices) != 2 || m.MinSalePrices[0] != (SaleMinimum{Chain: "solana-devnet", Asset: usdcMint, Amount: "20000000"}) ||
		m.MinSalePrices[1] != (SaleMinimum{Chain: "solana", Asset: "", Amount: "0"}) {
		t.Fatalf("market policy %+v", m)
	}
	if m, err := buildMarketPolicy(MarketPolicyInput{}); err != nil || m.CanSell || len(m.MinSalePrices) != 0 {
		t.Fatalf("empty: %+v %v", m, err)
	}
	bad := []SaleMinimumInput{
		{Chain: "nochain", Asset: usdcMint, Amount: "1"},
		{Chain: "solana-devnet", Asset: "not-an-address", Amount: "1"},
		{Chain: "solana-devnet", Asset: "So11111111111111111111111111111111111111112", Amount: "1"}, // unverified
	}
	for _, a := range []string{"-1", "1.5", "abc", " 1", "", strings.Repeat("9", 79)} {
		bad = append(bad, SaleMinimumInput{Chain: "solana-devnet", Asset: usdcMint, Amount: a})
	}
	for _, in := range bad {
		if _, err := buildMarketPolicy(MarketPolicyInput{CanSell: true, MinSalePrices: []SaleMinimumInput{in}}); err == nil {
			t.Fatalf("accepted %+v", in)
		}
	}
	dup := SaleMinimumInput{Chain: "solana-devnet", Asset: usdcMint, Amount: "1"}
	if _, err := buildMarketPolicy(MarketPolicyInput{MinSalePrices: []SaleMinimumInput{dup, dup}}); err == nil {
		t.Fatal("accepted a duplicate minimum")
	}
}
