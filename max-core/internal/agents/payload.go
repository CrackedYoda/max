package agents

import (
	"time"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/payments"
)

// AuthorizationPayload is exactly what MAX attests to. A signer must sign a
// transaction only if it matches these instructions field for field.
type AuthorizationPayload struct {
	Version          int                 `json:"version"`
	PaymentID        string              `json:"paymentId"`
	AgentID          string              `json:"agentId"`
	CredentialID     string              `json:"credentialId"`
	Decision         string              `json:"decision"`
	PolicyVersion    int                 `json:"policyVersion"`
	Chain            string              `json:"chain"`
	Sender           string              `json:"sender"`
	PayTo            string              `json:"payTo"`
	Recipient        string              `json:"recipient"`
	RecipientAddress string              `json:"recipientAddress"`
	Route            string              `json:"route"`
	Asset            payloadAsset        `json:"asset"`
	AmountBaseUnits  string              `json:"amountBaseUnits"`
	Instructions     payloadInstructions `json:"instructions"`
	IssuedAt         time.Time           `json:"issuedAt"`
	ExpiresAt        time.Time           `json:"expiresAt"`
}

type payloadAsset struct {
	Kind     string `json:"kind"`
	Address  string `json:"address,omitempty"`
	Decimals int    `json:"decimals"`
}

type payloadInstructions struct {
	EVM    *payloadEVM    `json:"evm,omitempty"`
	Solana *payloadSolana `json:"solana,omitempty"`
}

type payloadEVM struct {
	ChainID int64  `json:"chainId"`
	To      string `json:"to"`
	Value   string `json:"value"`
	Data    string `json:"data"`
}

type payloadSolana struct {
	Kind                    string `json:"kind"`
	To                      string `json:"to"`
	Lamports                string `json:"lamports,omitempty"`
	Mint                    string `json:"mint,omitempty"`
	TokenProgram            string `json:"tokenProgram,omitempty"`
	Decimals                int    `json:"decimals,omitempty"`
	Amount                  string `json:"amount,omitempty"`
	SourceTokenAccount      string `json:"sourceTokenAccount,omitempty"`
	DestinationTokenAccount string `json:"destinationTokenAccount,omitempty"`
}

func authorizationPayload(p payments.Payment, a TransferAuthorization, ins payments.Instructions, now time.Time) AuthorizationPayload {
	ns := p.Chain.Namespace
	recipientNS := ns
	if p.CrossChain != nil {
		recipientNS = p.CrossChain.DestinationChain.Namespace
	}
	out := AuthorizationPayload{
		Version: 1, PaymentID: p.ID.String(), AgentID: a.AgentID.String(), CredentialID: a.CredentialID.String(),
		Decision: string(a.Decision), PolicyVersion: a.PolicyVersion, Chain: p.Chain.Key,
		Sender:           chains.DisplayAddress(ns, p.SenderAddress),
		PayTo:            chains.DisplayAddress(ns, p.PayTo()),
		Recipient:        p.RecipientHandle,
		RecipientAddress: chains.DisplayAddress(recipientNS, p.RecipientAddress),
		Route:            string(p.Route),
		Asset:            payloadAsset{Kind: string(p.Asset.Kind), Decimals: p.Asset.Decimals},
		AmountBaseUnits:  p.Amount.String(),
		IssuedAt:         now.UTC().Truncate(time.Second),
		ExpiresAt:        p.ExpiresAt.UTC(),
	}
	if p.Asset.Address != "" {
		out.Asset.Address = chains.DisplayAddress(ns, p.Asset.Address)
	}
	if e := ins.EVM; e != nil {
		out.Instructions.EVM = &payloadEVM{ChainID: e.ChainID, To: e.To, Value: e.ValueHex, Data: e.Data}
	}
	if sl := ins.Solana; sl != nil {
		out.Instructions.Solana = &payloadSolana{
			Kind: string(sl.Kind), To: sl.To, Lamports: sl.Lamports, Mint: sl.Mint, TokenProgram: sl.TokenProgram,
			Decimals: sl.Decimals, Amount: sl.Amount, SourceTokenAccount: sl.SourceTokenAccount,
			DestinationTokenAccount: sl.DestinationTokenAccount,
		}
	}
	return out
}
