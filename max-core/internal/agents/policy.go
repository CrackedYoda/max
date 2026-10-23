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

	var reasons []string
	limit := findLimit(p.Limits, prop.Chain, prop.Asset)
	if limit == nil {
		reasons = append(reasons, fmt.Sprintf("no delegated limit for this asset on %s", prop.Chain))
	} else {
		if prop.Amount.Cmp(limit.MaxPerTransfer) > 0 {
			reasons = append(reasons, fmt.Sprintf("amount %s exceeds per-transfer limit %s", prop.Amount, limit.MaxPerTransfer))
		}
		if spent24h == nil {
			spent24h = new(big.Int)
		}
		if total := new(big.Int).Add(spent24h, prop.Amount); total.Cmp(limit.MaxPer24h) > 0 {
			reasons = append(reasons, fmt.Sprintf("24h spend would reach %s, above limit %s", total, limit.MaxPer24h))
		}
	}
	if !p.AllowAnyRecipient && !contains(p.AllowedRecipients, prop.Recipient) {
		reasons = append(reasons, fmt.Sprintf("recipient max.%s is not on the allowlist", prop.Recipient))
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
