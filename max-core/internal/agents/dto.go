package agents

import (
	"encoding/base64"
	"time"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
	"github.com/max/max-core/internal/wallets"
)

type createAgentRequest struct {
	Name        string `json:"name" binding:"required,max=64"`
	Description string `json:"description" binding:"max=280"`
}

type updateAgentRequest struct {
	Name        string `json:"name" binding:"max=64"`
	Description string `json:"description" binding:"max=280"`
	Status      string `json:"status" binding:"omitempty,oneof=active paused"`
}

type limitRequest struct {
	Chain          string `json:"chain" binding:"required,max=32"`
	Asset          string `json:"asset" binding:"max=128"`
	MaxPerTransfer string `json:"maxPerTransfer" binding:"required,max=100"`
	MaxPer24h      string `json:"maxPer24h" binding:"required,max=100"`
}

type policyRequest struct {
	Limits            []limitRequest `json:"limits" binding:"max=50,dive"`
	AllowedRecipients []string       `json:"allowedRecipients" binding:"max=100"`
	AllowAnyRecipient bool           `json:"allowAnyRecipient"`
}

type credentialRequest struct {
	Name          string   `json:"name" binding:"max=64"`
	Scopes        []string `json:"scopes" binding:"required,min=1,max=10"`
	ExpiresInDays int      `json:"expiresInDays" binding:"min=0,max=365"`
}

type claimIDRequest struct {
	Name        string `json:"name" binding:"required,max=64"`
	DisplayName string `json:"displayName" binding:"max=64"`
}

type walletChallengeRequest struct {
	Namespace string `json:"namespace" binding:"required,oneof=evm solana"`
	Address   string `json:"address" binding:"required,max=128"`
}

type walletVerifyRequest struct {
	Nonce     string `json:"nonce" binding:"required,len=32,hexadecimal"`
	Signature string `json:"signature" binding:"required,max=256"`
}

type denyRequest struct {
	Reason string `json:"reason" binding:"max=200"`
}

type proposeRequest struct {
	Recipient        string `json:"recipient" binding:"required,max=64"`
	Chain            string `json:"chain" binding:"required,max=32"`
	Asset            string `json:"asset" binding:"max=128"`
	Amount           string `json:"amount" binding:"required,max=100"`
	SenderAddress    string `json:"senderAddress" binding:"required,max=128"`
	IdempotencyKey   string `json:"idempotencyKey" binding:"required,min=8,max=64,printascii"`
	DestinationChain string `json:"destinationChain" binding:"max=32"`
	DestinationAsset string `json:"destinationAsset" binding:"max=128"`
	AmountType       string `json:"amountType" binding:"omitempty,oneof=input output"`
	Instruction      string `json:"instruction" binding:"max=1000"`
}

type agentResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

type limitResponse struct {
	Chain          string `json:"chain"`
	Asset          string `json:"asset"`
	MaxPerTransfer string `json:"maxPerTransfer"`
	MaxPer24h      string `json:"maxPer24h"`
	// Base units, for exact comparison by clients.
	MaxPerTransferBaseUnits string `json:"maxPerTransferBaseUnits"`
	MaxPer24hBaseUnits      string `json:"maxPer24hBaseUnits"`
}

type policyResponse struct {
	Version           int             `json:"version"`
	Limits            []limitResponse `json:"limits"`
	AllowedRecipients []string        `json:"allowedRecipients"`
	AllowAnyRecipient bool            `json:"allowAnyRecipient"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type credentialResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []Scope    `json:"scopes"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	// Secret is returned only when a key is issued or rotated.
	Secret string `json:"secret,omitempty"`
}

type agentDetailResponse struct {
	agentResponse
	Policy      policyResponse       `json:"policy"`
	Credentials []credentialResponse `json:"credentials"`
	Wallets     []map[string]any     `json:"wallets"`
	IDs         []map[string]any     `json:"ids"`
}

type authorizationResponse struct {
	Decision      string     `json:"decision"`
	Reasons       []string   `json:"reasons"`
	PolicyVersion int        `json:"policyVersion"`
	Instruction   string     `json:"instruction,omitempty"`
	DecidedAt     *time.Time `json:"decidedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}

type transferResponse struct {
	Payment       any                   `json:"payment"`
	Authorization authorizationResponse `json:"authorization"`
	Instructions  any                   `json:"instructions,omitempty"`
	Signed        *SignedAuthorization  `json:"signedAuthorization,omitempty"`
	Warnings      []string              `json:"warnings,omitempty"`
	Replayed      bool                  `json:"replayed,omitempty"`
}

func toAgentResponse(a Agent) agentResponse {
	return agentResponse{ID: a.AccountID.String(), Name: a.Name, Description: a.Description, Status: string(a.Status), CreatedAt: a.CreatedAt}
}

func toPolicyResponse(p Policy) policyResponse {
	out := policyResponse{Version: p.Version, AllowedRecipients: orEmpty(p.AllowedRecipients), AllowAnyRecipient: p.AllowAnyRecipient, UpdatedAt: p.UpdatedAt, Limits: []limitResponse{}}
	for i := range out.AllowedRecipients {
		out.AllowedRecipients[i] = maxids.Handle(out.AllowedRecipients[i])
	}
	for _, l := range p.Limits {
		chain, _ := chains.Lookup(l.Chain)
		asset, decimals := "native", chain.NativeDecimals
		if l.Asset != "" {
			asset = chains.DisplayAddress(chain.Namespace, l.Asset)
			if t, ok := chains.LookupVerifiedToken(l.Chain, l.Asset); ok {
				decimals = t.Decimals
			}
		}
		out.Limits = append(out.Limits, limitResponse{
			Chain: l.Chain, Asset: asset,
			MaxPerTransfer: payments.FormatAmount(l.MaxPerTransfer, decimals), MaxPer24h: payments.FormatAmount(l.MaxPer24h, decimals),
			MaxPerTransferBaseUnits: l.MaxPerTransfer.String(), MaxPer24hBaseUnits: l.MaxPer24h.String(),
		})
	}
	return out
}

func toCredentialResponse(c Credential) credentialResponse {
	return credentialResponse{
		ID: c.ID.String(), Name: c.Name, Prefix: "maxk_" + c.Prefix, Scopes: c.Scopes,
		ExpiresAt: c.ExpiresAt, LastUsedAt: c.LastUsedAt, RevokedAt: c.RevokedAt, CreatedAt: c.CreatedAt,
	}
}

func toAgentDetailResponse(d AgentDetail) agentDetailResponse {
	out := agentDetailResponse{
		agentResponse: toAgentResponse(d.Agent), Policy: toPolicyResponse(d.Policy),
		Credentials: []credentialResponse{}, Wallets: []map[string]any{}, IDs: []map[string]any{},
	}
	for _, c := range d.Credentials {
		out.Credentials = append(out.Credentials, toCredentialResponse(c))
	}
	for _, w := range d.Wallets {
		out.Wallets = append(out.Wallets, walletJSON(w))
	}
	for _, m := range d.IDs {
		out.IDs = append(out.IDs, map[string]any{"id": m.ID.String(), "handle": m.Handle(), "type": string(m.Type), "status": m.Status})
	}
	return out
}

func walletJSON(w wallets.Wallet) map[string]any {
	return map[string]any{"id": w.ID.String(), "namespace": string(w.Namespace), "address": chains.DisplayAddress(w.Namespace, w.Address), "verifiedAt": w.VerifiedAt}
}

func toTransferResponse(v TransferView) transferResponse {
	out := transferResponse{
		Payment: payments.RenderPayment(v.Payment),
		Authorization: authorizationResponse{
			Decision: string(v.Authorization.Decision), Reasons: orEmpty(v.Authorization.Reasons),
			PolicyVersion: v.Authorization.PolicyVersion, Instruction: v.Authorization.Instruction,
			DecidedAt: v.Authorization.DecidedAt, CreatedAt: v.Authorization.CreatedAt,
		},
		Signed: v.Signed, Warnings: v.Warnings, Replayed: v.Replayed,
	}
	if v.Instructions != nil {
		out.Instructions = payments.RenderInstructions(*v.Instructions)
	}
	return out
}

func publicKeyJSON(keyID string, pub []byte) map[string]any {
	return map[string]any{
		"keyId": keyID, "algorithm": "ed25519",
		"publicKey": base64.StdEncoding.EncodeToString(pub),
		"domain":    AuthorizationDomain,
	}
}
