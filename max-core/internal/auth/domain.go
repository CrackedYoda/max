package auth

import (
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
)

type Purpose string

const (
	PurposeLogin Purpose = "login"
	PurposeLink  Purpose = "link"
	// PurposeClaim proves a wallet holder asked for one specific name.
	PurposeClaim Purpose = "claim"
)

func (p Purpose) Valid() bool { return p == PurposeLogin || p == PurposeLink || p == PurposeClaim }

type Challenge struct {
	Nonce     string
	Purpose   Purpose
	Namespace chains.Namespace
	Address   string
	AccountID *uuid.UUID
	Message   string
	ExpiresAt time.Time
	// ClaimName is the canonical name a claim challenge is for.
	ClaimName string
}

// IDRef is a MAX ID as shown on the account: its id and max.<name> handle.
type IDRef struct {
	ID     uuid.UUID
	Handle string
}

// MeResult is the account plus its default and primary MAX IDs. Both refs are
// nil only when the account has no IDs yet (before the backfill has run).
type MeResult struct {
	Account      Account
	DefaultMaxID *IDRef
	PrimaryMaxID *IDRef
}

type Account struct {
	ID        uuid.UUID
	Type      string
	Status    string
	CreatedAt time.Time
}

type Wallet struct {
	ID         uuid.UUID
	AccountID  uuid.UUID
	Namespace  chains.Namespace
	Address    string
	VerifiedAt time.Time
}

type Session struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	AccountType string
	ExpiresAt   time.Time
}

type ChallengeInput struct {
	Purpose   Purpose
	Namespace string
	Address   string
	// Caller is required for link and claim challenges; the challenge is bound to it.
	Caller *uuid.UUID
	// Name is the name being claimed (claim challenges only).
	Name string
	// Origin is the requesting site's Origin header, if any.
	Origin string
}

type VerifyInput struct {
	Nonce     string
	Signature string
	Caller    *uuid.UUID
	IP        string
	UserAgent string
}

// ClaimProof is a signed claim challenge presented with a claim request.
type ClaimProof struct {
	Caller    uuid.UUID
	Name      string // canonical
	Nonce     string
	Signature string
}

type VerifyResult struct {
	Purpose        Purpose
	Account        Account
	Wallet         Wallet
	AccountCreated bool
	// Set only for login: the raw token is returned once and never stored.
	SessionToken string
	Session      *Session
}

type WalletProof struct {
	Namespace chains.Namespace
	Address   string
	Message   string
	Signature string
}
