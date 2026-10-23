package renames

import (
	"time"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
)

type renameRequest struct {
	NewName        string `json:"newName" binding:"required,max=64"`
	FeeChain       string `json:"feeChain" binding:"required,max=32"`
	FeeAsset       string `json:"feeAsset" binding:"max=128"`
	SenderAddress  string `json:"senderAddress" binding:"required,max=128"`
	IdempotencyKey string `json:"idempotencyKey" binding:"required,min=8,max=64,printascii"`
}

type feeOptionResponse struct {
	Chain     chains.Chain `json:"chain"`
	Asset     string       `json:"asset"`
	Symbol    string       `json:"symbol"`
	Amount    string       `json:"amount"`
	BaseUnits string       `json:"baseUnits"`
}

type requestResponse struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	OldHandle     string     `json:"oldHandle"`
	NewHandle     string     `json:"newHandle"`
	FailureReason string     `json:"failureReason,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

type eligibilityResponse struct {
	Handle        string              `json:"handle"`
	Eligible      bool                `json:"eligible"`
	Reason        string              `json:"reason,omitempty"`
	AvailableAt   time.Time           `json:"renameAvailableAt"`
	CooldownHours int                 `json:"cooldownHours"`
	Fees          []feeOptionResponse `json:"fees"`
	Pending       *requestResponse    `json:"pending,omitempty"`
}

type viewResponse struct {
	Request      requestResponse `json:"request"`
	FeePayment   any             `json:"feePayment"`
	Instructions any             `json:"instructions,omitempty"`
	Warnings     []string        `json:"warnings,omitempty"`
}

type historyResponse struct {
	OldHandle    string    `json:"oldHandle"`
	NewHandle    string    `json:"newHandle"`
	FeePaymentID string    `json:"feePaymentId,omitempty"`
	ChangedAt    time.Time `json:"changedAt"`
}

func toFeeOptions(fees []FeeOption) []feeOptionResponse {
	out := make([]feeOptionResponse, 0, len(fees))
	for _, f := range fees {
		asset := "native"
		if f.Asset != "" {
			asset = chains.DisplayAddress(f.Chain.Namespace, f.Asset)
		}
		out = append(out, feeOptionResponse{
			Chain: f.Chain, Asset: asset, Symbol: f.Symbol,
			Amount: payments.FormatAmount(f.Amount, f.Decimals), BaseUnits: f.Amount.String(),
		})
	}
	return out
}

func toRequestResponse(r Request) requestResponse {
	return requestResponse{
		ID: r.ID.String(), Status: string(r.Status), OldHandle: maxids.Handle(r.OldName), NewHandle: maxids.Handle(r.NewName),
		FailureReason: r.FailureReason, CreatedAt: r.CreatedAt, CompletedAt: r.CompletedAt,
	}
}

func toEligibilityResponse(e Eligibility) eligibilityResponse {
	out := eligibilityResponse{
		Handle: maxids.Handle(e.MaxID.Name), Eligible: e.Eligible, Reason: e.Reason, AvailableAt: e.AvailableAt,
		CooldownHours: int(e.Cooldown.Hours()), Fees: toFeeOptions(e.Fees),
	}
	if e.Pending != nil {
		p := toRequestResponse(*e.Pending)
		out.Pending = &p
	}
	return out
}

func toViewResponse(v RequestView) viewResponse {
	out := viewResponse{Request: toRequestResponse(v.Request), FeePayment: payments.RenderPayment(v.Payment), Warnings: v.Warnings}
	if v.Instructions != nil {
		out.Instructions = payments.RenderInstructions(*v.Instructions)
	}
	return out
}
