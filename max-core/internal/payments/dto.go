package payments

import (
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/routing"
)

type prepareRequest struct {
	Recipient         string `json:"recipient" binding:"required,max=64"`
	Chain             string `json:"chain" binding:"required,max=32"`
	Asset             string `json:"asset" binding:"max=128"`
	Amount            string `json:"amount" binding:"required,max=100"`
	SenderAddress     string `json:"senderAddress" binding:"required,max=128"`
	IdempotencyKey    string `json:"idempotencyKey" binding:"required,min=8,max=64,printascii"`
	ResolutionVersion *int64 `json:"resolutionVersion" binding:"omitempty,min=1"`
	DestinationChain  string `json:"destinationChain" binding:"max=32"`
	DestinationAsset  string `json:"destinationAsset" binding:"max=128"`
	AmountType        string `json:"amountType" binding:"omitempty,oneof=input output"`
	SlippageBps       *int   `json:"slippageBps"`
}

type submitRequest struct {
	TxHash string `json:"txHash" binding:"required,max=128"`
}

type recipientResponse struct {
	MaxID             string `json:"maxId,omitempty"`
	Handle            string `json:"handle"`
	Address           string `json:"address"`
	ResolutionVersion int64  `json:"resolutionVersion"`
}

type assetResponse struct {
	Kind         string `json:"kind"`
	Address      string `json:"address,omitempty"`
	Symbol       string `json:"symbol,omitempty"`
	Decimals     int    `json:"decimals"`
	Verified     bool   `json:"verified"`
	TokenProgram string `json:"tokenProgram,omitempty"`
}

type amountResponse struct {
	Display   string `json:"display"`
	BaseUnits string `json:"baseUnits"`
}

type evmInstructionResponse struct {
	ChainID int64  `json:"chainId"`
	From    string `json:"from"`
	To      string `json:"to"`
	Value   string `json:"value"`
	Data    string `json:"data"`
}

type solanaInstructionResponse struct {
	Kind                          string `json:"kind"`
	From                          string `json:"from"`
	To                            string `json:"to"`
	Lamports                      string `json:"lamports,omitempty"`
	Mint                          string `json:"mint,omitempty"`
	TokenProgram                  string `json:"tokenProgram,omitempty"`
	Decimals                      *int   `json:"decimals,omitempty"`
	Amount                        string `json:"amount,omitempty"`
	SourceTokenAccount            string `json:"sourceTokenAccount,omitempty"`
	DestinationTokenAccount       string `json:"destinationTokenAccount,omitempty"`
	DestinationTokenAccountExists *bool  `json:"destinationTokenAccountExists,omitempty"`
}

type instructionsResponse struct {
	EVM    *evmInstructionResponse    `json:"evm,omitempty"`
	Solana *solanaInstructionResponse `json:"solana,omitempty"`
}

type eventResponse struct {
	Type       string         `json:"type"`
	FromStatus string         `json:"fromStatus,omitempty"`
	ToStatus   string         `json:"toStatus"`
	Detail     map[string]any `json:"detail,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type crossChainResponse struct {
	Provider            string          `json:"provider"`
	SwapType            string          `json:"swapType"`
	DestinationChain    chains.Chain    `json:"destinationChain"`
	DestinationAsset    assetResponse   `json:"destinationAsset"`
	DepositAddress      string          `json:"depositAddress"`
	RefundTo            string          `json:"refundTo"`
	AmountIn            amountResponse  `json:"amountIn"`
	ExpectedAmountOut   amountResponse  `json:"expectedAmountOut"`
	MinAmountOut        amountResponse  `json:"minAmountOut"`
	AmountInUSD         string          `json:"amountInUsd"`
	AmountOutUSD        string          `json:"amountOutUsd"`
	SlippageBps         int             `json:"slippageBps"`
	TimeEstimateSeconds int             `json:"timeEstimateSeconds"`
	QuoteDeadline       time.Time       `json:"quoteDeadline"`
	DepositConfirmed    bool            `json:"depositConfirmed"`
	ProviderStatus      string          `json:"providerStatus,omitempty"`
	AmountOut           *amountResponse `json:"amountOut,omitempty"`
	RefundedAmount      *amountResponse `json:"refundedAmount,omitempty"`
	RefundReason        string          `json:"refundReason,omitempty"`
	DestinationTxs      []routing.TxRef `json:"destinationTxs"`
}

type paymentResponse struct {
	ID                    string                `json:"id"`
	Purpose               string                `json:"purpose"`
	Status                string                `json:"status"`
	StatusReason          string                `json:"statusReason,omitempty"`
	Route                 string                `json:"route"`
	Recipient             recipientResponse     `json:"recipient"`
	CrossChain            *crossChainResponse   `json:"crossChain,omitempty"`
	SenderAddress         string                `json:"senderAddress"`
	Chain                 chains.Chain          `json:"chain"`
	Asset                 assetResponse         `json:"asset"`
	Amount                amountResponse        `json:"amount"`
	TxHash                string                `json:"txHash,omitempty"`
	ExplorerURL           string                `json:"explorerUrl,omitempty"`
	Confirmations         int64                 `json:"confirmations"`
	RequiredConfirmations int64                 `json:"requiredConfirmations"`
	ExpiresAt             time.Time             `json:"expiresAt"`
	SubmittedAt           *time.Time            `json:"submittedAt,omitempty"`
	CompletedAt           *time.Time            `json:"completedAt,omitempty"`
	CreatedAt             time.Time             `json:"createdAt"`
	Instructions          *instructionsResponse `json:"instructions,omitempty"`
	Warnings              []string              `json:"warnings,omitempty"`
	Replayed              bool                  `json:"replayed,omitempty"`
	Events                []eventResponse       `json:"events,omitempty"`
}

func toPaymentResponse(p Payment) paymentResponse {
	ns := p.Chain.Namespace
	resp := paymentResponse{
		ID: p.ID.String(), Purpose: string(purposeOr(p.Purpose)), Status: string(p.Status), StatusReason: p.StatusReason, Route: string(p.Route),
		Recipient: recipientResponse{
			Handle:  p.RecipientHandle,
			Address: chains.DisplayAddress(ns, p.RecipientAddress), ResolutionVersion: p.ResolutionVersion,
		},
		SenderAddress: chains.DisplayAddress(ns, p.SenderAddress),
		Chain:         p.Chain,
		Asset: assetResponse{
			Kind: string(p.Asset.Kind), Symbol: p.Asset.Symbol, Decimals: p.Asset.Decimals,
			Verified: p.Asset.Verified, TokenProgram: p.Asset.TokenProgram,
		},
		Amount:                amountResponse{Display: FormatAmount(p.Amount, p.Asset.Decimals), BaseUnits: p.Amount.String()},
		TxHash:                p.TxHash,
		Confirmations:         p.Confirmations,
		RequiredConfirmations: p.RequiredConfirmations,
		ExpiresAt:             p.ExpiresAt,
		SubmittedAt:           p.SubmittedAt,
		CompletedAt:           p.CompletedAt,
		CreatedAt:             p.CreatedAt,
	}
	if p.Asset.Address != "" {
		resp.Asset.Address = chains.DisplayAddress(ns, p.Asset.Address)
	}
	if p.MaxID != uuid.Nil {
		resp.Recipient.MaxID = p.MaxID.String()
	}
	if p.TxHash != "" {
		resp.ExplorerURL = p.Chain.TxURL(p.TxHash)
	}
	if r := p.CrossChain; r != nil {
		dns := r.DestinationChain.Namespace
		resp.Recipient.Address = chains.DisplayAddress(dns, p.RecipientAddress)
		amt := func(v *big.Int, decimals int) amountResponse {
			return amountResponse{Display: FormatAmount(v, decimals), BaseUnits: v.String()}
		}
		cc := &crossChainResponse{
			Provider: r.Provider, SwapType: string(r.SwapType), DestinationChain: r.DestinationChain,
			DestinationAsset: assetResponse{
				Kind: string(r.DestinationAsset.Kind), Symbol: r.DestinationAsset.Symbol,
				Decimals: r.DestinationAsset.Decimals, Verified: r.DestinationAsset.Verified,
			},
			DepositAddress:    chains.DisplayAddress(ns, r.DepositAddress),
			RefundTo:          chains.DisplayAddress(ns, r.RefundTo),
			AmountIn:          amt(r.AmountIn, p.Asset.Decimals),
			ExpectedAmountOut: amt(r.AmountOutExpected, r.DestinationAsset.Decimals),
			MinAmountOut:      amt(r.MinAmountOut, r.DestinationAsset.Decimals),
			AmountInUSD:       r.AmountInUSD, AmountOutUSD: r.AmountOutUSD, SlippageBps: r.SlippageBps,
			TimeEstimateSeconds: int(r.TimeEstimate.Seconds()), QuoteDeadline: r.QuoteDeadline,
			DepositConfirmed: r.DepositConfirmedAt != nil, ProviderStatus: r.ProviderStatus,
			RefundReason: r.RefundReason, DestinationTxs: r.DestinationTxs,
		}
		if r.DestinationAsset.Address != "" {
			cc.DestinationAsset.Address = chains.DisplayAddress(dns, r.DestinationAsset.Address)
		}
		if r.AmountOutActual != nil {
			a := amt(r.AmountOutActual, r.DestinationAsset.Decimals)
			cc.AmountOut = &a
		}
		if r.RefundedAmount != nil {
			a := amt(r.RefundedAmount, p.Asset.Decimals)
			cc.RefundedAmount = &a
		}
		if cc.DestinationTxs == nil {
			cc.DestinationTxs = []routing.TxRef{}
		}
		resp.CrossChain = cc
	}
	return resp
}

func toInstructionsResponse(ins Instructions) *instructionsResponse {
	out := &instructionsResponse{}
	if e := ins.EVM; e != nil {
		out.EVM = &evmInstructionResponse{ChainID: e.ChainID, From: e.From, To: e.To, Value: e.ValueHex, Data: e.Data}
	}
	if s := ins.Solana; s != nil {
		r := &solanaInstructionResponse{Kind: string(s.Kind), From: s.From, To: s.To, Lamports: s.Lamports}
		if s.Kind == SolanaSPLTransferChecked {
			d := s.Decimals
			r.Mint, r.TokenProgram, r.Decimals, r.Amount = s.Mint, s.TokenProgram, &d, s.Amount
			r.SourceTokenAccount, r.DestinationTokenAccount = s.SourceTokenAccount, s.DestinationTokenAccount
			r.DestinationTokenAccountExists = s.DestinationTokenAccountExists
		}
		out.Solana = r
	}
	return out
}

func toEventResponses(events []Event) []eventResponse {
	out := make([]eventResponse, 0, len(events))
	for _, e := range events {
		out = append(out, eventResponse{
			Type: e.Type, FromStatus: string(e.FromStatus), ToStatus: string(e.ToStatus),
			Detail: e.Detail, CreatedAt: e.CreatedAt,
		})
	}
	return out
}

// RenderPayment exposes the canonical payment JSON for other features' responses.
func RenderPayment(p Payment) any { return toPaymentResponse(p) }

func RenderInstructions(ins Instructions) any { return toInstructionsResponse(ins) }
