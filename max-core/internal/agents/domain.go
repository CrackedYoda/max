package agents

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

type Scope string

const (
	ScopePaymentsRead       Scope = "payments:read"
	ScopePaymentsPropose    Scope = "payments:propose"
	ScopePaymentsSubmit     Scope = "payments:submit"
	ScopeAuthorizationsRead Scope = "authorizations:read" // for the agent's signer
	ScopeWalletsLink        Scope = "wallets:link"
	ScopeIDsWrite           Scope = "ids:write"
	ScopeMarketRead         Scope = "market:read"
	ScopeMarketSell         Scope = "market:sell"
	ScopeMarketBuy          Scope = "market:buy"
)

var AllScopes = []Scope{
	ScopePaymentsRead, ScopePaymentsPropose, ScopePaymentsSubmit,
	ScopeAuthorizationsRead, ScopeWalletsLink, ScopeIDsWrite,
	ScopeMarketRead, ScopeMarketSell, ScopeMarketBuy,
}

func ValidScope(s string) bool {
	for _, v := range AllScopes {
		if string(v) == s {
			return true
		}
	}
	return false
}

type Status string

const (
	StatusActive Status = "active"
	StatusPaused Status = "paused"
)

type Agent struct {
	AccountID       uuid.UUID
	AdministratorID uuid.UUID
	Name            string
	Description     string
	Status          Status
	CreatedAt       time.Time
}

// AssetLimit grants delegated authority for one asset on one chain.
type AssetLimit struct {
	Chain          string
	Asset          string // canonical contract/mint; "" = native
	MaxPerTransfer *big.Int
	MaxPer24h      *big.Int
}

type Policy struct {
	Version           int
	Limits            []AssetLimit
	AllowedRecipients []string // canonical MAX ID names
	AllowAnyRecipient bool
	Market            MarketPolicy
	UpdatedAt         time.Time
}

// MarketPolicy governs selling on the marketplace (spec §8). Buying is
// governed by Limits, like transfers.
type MarketPolicy struct {
	CanSell bool
	// MinSalePrice is in the sale asset's base units; "0" means no minimum.
	MinSalePrice string
}

type Credential struct {
	ID         uuid.UUID
	AgentID    uuid.UUID
	Name       string
	Prefix     string
	Scopes     []Scope
	CreatedBy  uuid.UUID
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	ReplacedBy *uuid.UUID
	CreatedAt  time.Time
}

// IssuedCredential carries the secret, which is shown exactly once.
type IssuedCredential struct {
	Credential
	Secret string
}

type DecisionOutcome string

const (
	DecisionAutoApproved    DecisionOutcome = "auto_approved"
	DecisionPendingApproval DecisionOutcome = "pending_approval"
	DecisionApproved        DecisionOutcome = "approved"
	DecisionDenied          DecisionOutcome = "denied"
)

type TransferAuthorization struct {
	PaymentID     uuid.UUID
	AgentID       uuid.UUID
	CredentialID  uuid.UUID
	Decision      DecisionOutcome
	Reasons       []string
	PolicyVersion int
	Chain         string
	AssetAddress  string
	Amount        *big.Int
	Instruction   string
	DecidedBy     *uuid.UUID
	DecidedAt     *time.Time
	CreatedAt     time.Time
}

// CredentialPrincipal is what credential authentication yields.
type CredentialPrincipal struct {
	AgentID      uuid.UUID
	CredentialID uuid.UUID
	Scopes       []Scope
}
