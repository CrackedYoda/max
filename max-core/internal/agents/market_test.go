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
		Market: MarketPolicy{CanSell: true, MinSalePrice: usdc(20).String()},
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

func TestBuildMarketPolicy(t *testing.T) {
	ok := []MarketPolicyInput{{CanSell: true, MinSalePrice: "20000000"}, {CanSell: false}, {MinSalePrice: "0"}}
	for _, in := range ok {
		if _, err := buildMarketPolicy(in); err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
	}
	m, _ := buildMarketPolicy(MarketPolicyInput{CanSell: true})
	if m.MinSalePrice != "0" {
		t.Fatalf("default min: %+v", m)
	}
	for _, bad := range []string{"-1", "1.5", "abc", " 1", strings.Repeat("9", 79)} {
		if _, err := buildMarketPolicy(MarketPolicyInput{CanSell: true, MinSalePrice: bad}); err == nil {
			t.Fatalf("accepted minSalePrice %q", bad)
		}
	}
}
