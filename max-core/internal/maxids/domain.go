package maxids

import (
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
)

type Type string

const (
	TypeHuman Type = "human"
	TypeAgent Type = "agent"
)

func (t Type) Valid() bool { return t == TypeHuman || t == TypeAgent }

type ClaimPolicy string

const (
	PolicyBlocked      ClaimPolicy = "blocked"
	PolicyXOAuth       ClaimPolicy = "x_oauth"
	PolicyManualReview ClaimPolicy = "manual_review"
)

type MaxID struct {
	ID             uuid.UUID
	CanonicalName  string
	OwnerAccountID uuid.UUID
	Type           Type
	Status         string
	// IsDefault marks the account's auto-created receiving ID.
	IsDefault         bool
	DisplayName       string
	Description       string
	AvatarURL         string
	ResolutionVersion int64
	RenameAvailableAt time.Time
	CreatedAt         time.Time
	Destinations      []Destination
	Settlement        *Settlement
}

// Settlement is the recipient's opt-in conversion target: when a sender pays
// on a chain this ID has no wallet for, MAX may offer a quoted cross-chain
// route that delivers this asset instead.
type Settlement struct {
	Chain     string
	Asset     string // canonical contract/mint; empty = native coin
	Enabled   bool
	UpdatedAt time.Time
}

func (m MaxID) Handle() string { return Handle(m.CanonicalName) }

type Destination struct {
	Namespace chains.Namespace
	// WalletID is the verified wallet behind the destination; nil when the
	// owner pasted the address instead.
	WalletID  *uuid.UUID
	Address   string
	Pasted    bool
	UpdatedAt time.Time
}

type Reserved struct {
	CanonicalName string
	Policy        ClaimPolicy
	XHandle       string
}

type Availability struct {
	CanonicalName string
	Handle        string
	Available     bool
	// Reason is empty when available; otherwise taken | pending_rename |
	// quarantined | reserved | invalid.
	Reason      string
	Detail      string
	ClaimPolicy ClaimPolicy
	// XHandle is the X account that can claim an x_oauth or manual_review name.
	XHandle string
}

type ClaimInput struct {
	Name        string
	Type        Type
	DisplayName string
}

type ProfileInput struct {
	DisplayName string
	Description string
	AvatarURL   string
}
