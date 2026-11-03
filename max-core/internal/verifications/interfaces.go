package verifications

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/xapi"
)

type XClient interface {
	Enabled() bool
	AuthorizeURL(state, challenge string) string
	Exchange(ctx context.Context, code, verifier string) (string, error)
	Me(ctx context.Context, token string) (xapi.User, error)
	Revoke(ctx context.Context, token string) error
}

type ClaimParams struct {
	Reserved      Reserved
	AccountID     uuid.UUID
	Verification  Verification
	DisplayName   string
	VerifiedUntil time.Time
}

type Repository interface {
	CreateState(ctx context.Context, stateHash []byte, account, session uuid.UUID, verifier string, expires time.Time) error
	ConsumeState(ctx context.Context, stateHash []byte) (OAuthState, error)
	// SaveVerification records a fresh verification and renews every live
	// claim backed by the same X user id, unlocking lapsed IDs.
	SaveVerification(ctx context.Context, account uuid.UUID, u xapi.User, renewUntil time.Time) (Verification, []Claim, error)
	ListVerifications(ctx context.Context, account uuid.UUID) ([]Verification, error)
	LatestVerification(ctx context.Context, account uuid.UUID) (*Verification, error)
	// RevokeVerification revokes; account nil means an administrator action.
	RevokeVerification(ctx context.Context, id uuid.UUID, account *uuid.UUID, reason string, actor uuid.UUID) error

	GetReserved(ctx context.Context, canonical string) (*Reserved, error)
	ClaimActive(ctx context.Context, p ClaimParams) (Claim, error)
	CreatePendingClaim(ctx context.Context, p ClaimParams) (Claim, error)
	ListClaimsByAccount(ctx context.Context, account uuid.UUID) ([]Claim, error)
	ListClaimsByStatus(ctx context.Context, status ClaimStatus, limit int32) ([]Claim, error)
	ApproveClaim(ctx context.Context, id, admin uuid.UUID, verifiedUntil time.Time, note string) (Claim, error)
	DenyClaim(ctx context.Context, id, admin uuid.UUID, note string) (Claim, error)
	RevokeClaim(ctx context.Context, id, admin uuid.UUID, note string) (Claim, error)
	// LapseOverdue lapses claims past verified_until + grace and locks their IDs.
	LapseOverdue(ctx context.Context, grace time.Duration) ([]Claim, error)
}

type Service interface {
	// StartX begins X OAuth. native flows hand the callback back to the app
	// (NativeResultRedirect), which completes them with its own session.
	StartX(ctx context.Context, account uuid.UUID, session uuid.UUID, native bool) (authorizeURL string, expires time.Time, err error)
	CompleteX(ctx context.Context, account, session *uuid.UUID, state, code, oauthError string) (Verification, []Claim, error)
	ListVerifications(ctx context.Context, account uuid.UUID) ([]Verification, error)
	RevokeMine(ctx context.Context, account, id uuid.UUID) error

	ClaimReserved(ctx context.Context, account uuid.UUID, name, displayName string) (ClaimResult, error)
	ListClaims(ctx context.Context, account uuid.UUID) ([]Claim, error)

	AdminListClaims(ctx context.Context, status ClaimStatus) ([]Claim, error)
	AdminApprove(ctx context.Context, admin, id uuid.UUID, note string) (Claim, error)
	AdminDeny(ctx context.Context, admin, id uuid.UUID, note string) (Claim, error)
	AdminRevokeClaim(ctx context.Context, admin, id uuid.UUID, note string) (Claim, error)
	AdminRevokeVerification(ctx context.Context, admin, id uuid.UUID, note string) error

	Sweep(ctx context.Context) (int, error)
	ResultRedirect() string
	NativeResultRedirect() string
}
