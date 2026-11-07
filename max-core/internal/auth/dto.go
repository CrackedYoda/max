package auth

import (
	"time"

	"github.com/max/max-core/internal/chains"
)

type challengeRequest struct {
	Purpose   string `json:"purpose" binding:"required,oneof=login link"`
	Namespace string `json:"namespace" binding:"required,oneof=evm solana"`
	Address   string `json:"address" binding:"required,max=128"`
}

type challengeResponse struct {
	Nonce     string    `json:"nonce"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type verifyRequest struct {
	Nonce     string `json:"nonce" binding:"required,len=32,hexadecimal"`
	Signature string `json:"signature" binding:"required,max=256"`
	// TokenDelivery "body" returns the session token in the response for
	// native apps that keep it in secure storage; browsers use the cookie.
	TokenDelivery string `json:"tokenDelivery" binding:"omitempty,oneof=cookie body"`
}

type accountResponse struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type idRefResponse struct {
	ID     string `json:"id"`
	Handle string `json:"handle"`
}

type meResponse struct {
	accountResponse
	DefaultMaxID *idRefResponse `json:"defaultMaxId"`
	PrimaryMaxID *idRefResponse `json:"primaryMaxId"`
}

func toIDRefResponse(r *IDRef) *idRefResponse {
	if r == nil {
		return nil
	}
	return &idRefResponse{ID: r.ID.String(), Handle: r.Handle}
}

func toMeResponse(m MeResult) meResponse {
	return meResponse{
		accountResponse: toAccountResponse(m.Account),
		DefaultMaxID:    toIDRefResponse(m.DefaultMaxID),
		PrimaryMaxID:    toIDRefResponse(m.PrimaryMaxID),
	}
}

type walletResponse struct {
	ID         string    `json:"id"`
	Namespace  string    `json:"namespace"`
	Address    string    `json:"address"`
	VerifiedAt time.Time `json:"verifiedAt"`
}

type verifyResponse struct {
	Purpose        string          `json:"purpose"`
	Account        accountResponse `json:"account"`
	Wallet         walletResponse  `json:"wallet"`
	AccountCreated bool            `json:"accountCreated"`
	SessionExpires *time.Time      `json:"sessionExpiresAt,omitempty"`
	SessionToken   string          `json:"sessionToken,omitempty"`
}

func toAccountResponse(a Account) accountResponse {
	return accountResponse{ID: a.ID.String(), Type: a.Type, Status: a.Status, CreatedAt: a.CreatedAt}
}

func toWalletResponse(w Wallet) walletResponse {
	return walletResponse{
		ID: w.ID.String(), Namespace: string(w.Namespace),
		Address: chains.DisplayAddress(w.Namespace, w.Address), VerifiedAt: w.VerifiedAt,
	}
}
