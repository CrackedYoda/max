package verifications

import (
	"time"

	"github.com/google/uuid"
)

const ProviderX = "x"

// Verification proves an account controlled an X account at VerifiedAt. It is
// not a trademark right or a permanent identity claim.
type Verification struct {
	ID             uuid.UUID
	AccountID      uuid.UUID
	Provider       string
	ProviderUserID string
	Username       string
	DisplayName    string
	VerifiedAt     time.Time
	RevokedAt      *time.Time
	RevokeReason   string
}

type ClaimStatus string

const (
	ClaimPendingReview ClaimStatus = "pending_review"
	ClaimActive        ClaimStatus = "active"
	ClaimLapsed        ClaimStatus = "lapsed"
	ClaimDenied        ClaimStatus = "denied"
	ClaimRevoked       ClaimStatus = "revoked"
)

type Claim struct {
	ID             uuid.UUID
	ReservedNameID uuid.UUID
	CanonicalName  string
	AccountID      uuid.UUID
	MaxID          *uuid.UUID
	XUserID        string
	XUsername      string
	Status         ClaimStatus
	VerifiedUntil  *time.Time
	ReviewNote     string
	DecidedAt      *time.Time
	CreatedAt      time.Time
}

type Reserved struct {
	ID            uuid.UUID
	CanonicalName string
	Policy        string // blocked | x_oauth | manual_review
	XHandle       string
}

type OAuthState struct {
	AccountID uuid.UUID
	SessionID uuid.UUID
	Verifier  string
}

type Config struct {
	StateTTL time.Duration
	// ClaimFreshness: a reserved name may only be claimed with a verification
	// this recent, because OAuth proves control only at verification time.
	ClaimFreshness time.Duration
	// ReverifyInterval is how long a claim stays valid before the same X
	// account must verify again; ReverifyGrace is the lapse grace after that.
	ReverifyInterval time.Duration
	ReverifyGrace    time.Duration
	// ResultRedirect is the fixed frontend URL the OAuth callback redirects to.
	ResultRedirect string
}

func DefaultConfig() Config {
	return Config{
		StateTTL:         10 * time.Minute,
		ClaimFreshness:   15 * time.Minute,
		ReverifyInterval: 180 * 24 * time.Hour,
		ReverifyGrace:    14 * 24 * time.Hour,
	}
}

type ClaimResult struct {
	Claim  Claim
	Handle string
	// Pending is true for manual_review names awaiting an administrator.
	Pending bool
}
