package agents

import (
	"fmt"
	"math/big"
)

// Proposal is the deterministic input to policy evaluation. It is built from
// the resolved payment intent, never from the agent's natural-language text.
type Proposal struct {
	Chain     string
	Asset     string // canonical; "" = native
	Amount    *big.Int
	Recipient string // canonical MAX ID name
}

type Evaluation struct {
	// Outcome is auto_approved, pending_approval or denied.
	Outcome DecisionOutcome
	Reasons []string
}

// Evaluate decides whether a proposal falls inside the agent's delegated
// authority. spent24h is what this agent already committed in the same asset
// over the rolling window. Anything outside authority needs a human; nothing
// here can be influenced by prompt content.
func Evaluate(p Policy, status Status, prop Proposal, spent24h *big.Int) Evaluation {
	if status != StatusActive {
		return Evaluation{Outcome: DecisionDenied, Reasons: []string{"agent is paused"}}
	}
	if prop.Amount == nil || prop.Amount.Sign() <= 0 {
		return Evaluation{Outcome: DecisionDenied, Reasons: []string{"amount must be positive"}}
	}

	reasons := limitReasons(p, prop.Chain, prop.Asset, prop.Amount, spent24h)
	if !p.AllowAnyRecipient && !contains(p.AllowedRecipients, prop.Recipient) {
		reasons = append(reasons, fmt.Sprintf("recipient max.%s is not on the allowlist", prop.Recipient))
	}

	if len(reasons) > 0 {
		return Evaluation{Outcome: DecisionPendingApproval, Reasons: reasons}
	}
	return Evaluation{Outcome: DecisionAutoApproved}
}

// limitReasons applies the per-transfer and rolling 24h limits for one asset.
func limitReasons(p Policy, chain, asset string, amount, spent24h *big.Int) []string {
	var reasons []string
	limit := findLimit(p.Limits, chain, asset)
	if limit == nil {
		return append(reasons, fmt.Sprintf("no delegated limit for this asset on %s", chain))
	}
	if amount.Cmp(limit.MaxPerTransfer) > 0 {
		reasons = append(reasons, fmt.Sprintf("amount %s exceeds per-transfer limit %s", amount, limit.MaxPerTransfer))
	}
	if spent24h == nil {
		spent24h = new(big.Int)
	}
	if total := new(big.Int).Add(spent24h, amount); total.Cmp(limit.MaxPer24h) > 0 {
		reasons = append(reasons, fmt.Sprintf("24h spend would reach %s, above limit %s", total, limit.MaxPer24h))
	}
	return reasons
}

// Market actions evaluated by EvaluateMarket.
const (
	MarketActionBuy    = "buy"
	MarketActionOffer  = "offer"
	MarketActionPay    = "pay"
	MarketActionList   = "list"
	MarketActionAccept = "accept"
)

// EvaluateMarket decides a marketplace action (spec §8). Buy, offer and pay
// spend the asset and use the same per-transfer and rolling 24h limits as
// transfers; spent24h already includes transfer and market spend. List and
// accept sell, and need market.canSell and amount >= market.minSalePrice.
func EvaluateMarket(p Policy, status Status, action string, amount *big.Int, chain, asset string, spent24h *big.Int) Evaluation {
	if status != StatusActive {
		return Evaluation{Outcome: DecisionDenied, Reasons: []string{"agent is paused"}}
	}
	if amount == nil || amount.Sign() <= 0 {
		return Evaluation{Outcome: DecisionDenied, Reasons: []string{"amount must be positive"}}
	}
	var reasons []string
	switch action {
	case MarketActionBuy, MarketActionOffer, MarketActionPay:
		reasons = limitReasons(p, chain, asset, amount, spent24h)
	case MarketActionList, MarketActionAccept:
		if !p.Market.CanSell {
			reasons = append(reasons, "agent is not allowed to sell on the marketplace")
		}
		if min, ok := new(big.Int).SetString(p.Market.MinSalePrice, 10); ok && amount.Cmp(min) < 0 {
			reasons = append(reasons, fmt.Sprintf("price %s is below the minimum sale price %s", amount, min))
		}
	default:
		return Evaluation{Outcome: DecisionDenied, Reasons: []string{"unknown market action " + action}}
	}
	if len(reasons) > 0 {
		return Evaluation{Outcome: DecisionPendingApproval, Reasons: reasons}
	}
	return Evaluation{Outcome: DecisionAutoApproved}
}

func findLimit(limits []AssetLimit, chain, asset string) *AssetLimit {
	for i := range limits {
		if limits[i].Chain == chain && limits[i].Asset == asset {
			return &limits[i]
		}
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
