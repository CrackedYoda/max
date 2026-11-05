package verifications

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
	"github.com/max/max-core/internal/xapi"
)

type repository struct {
	pool *pgxpool.Pool
	q    *generated.Queries
}

var _ Repository = (*repository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &repository{pool: pool, q: generated.New(pool)}
}

func (r *repository) CreateState(ctx context.Context, hash []byte, account, session uuid.UUID, verifier string, expires time.Time) error {
	if err := r.q.CreateOAuthState(ctx, generated.CreateOAuthStateParams{
		StateHash: hash, Provider: ProviderX, AccountID: account, SessionID: session, CodeVerifier: verifier, ExpiresAt: expires,
	}); err != nil {
		return shared.ErrInternal.WithError(err)
	}
	return nil
}

func (r *repository) ConsumeState(ctx context.Context, hash []byte) (OAuthState, error) {
	row, err := r.q.ConsumeOAuthState(ctx, hash)
	if err != nil {
		if database.IsNoRows(err) {
			return OAuthState{}, ErrOAuthStateInvalid
		}
		return OAuthState{}, shared.ErrInternal.WithError(err)
	}
	return OAuthState{AccountID: row.AccountID, SessionID: row.SessionID, Verifier: row.CodeVerifier}, nil
}

func (r *repository) SaveVerification(ctx context.Context, account uuid.UUID, u xapi.User, renewUntil time.Time) (Verification, []Claim, error) {
	var v Verification
	var renewed []Claim
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		existing, err := q.GetActiveVerificationForProviderUser(ctx, generated.GetActiveVerificationForProviderUserParams{Provider: ProviderX, ProviderUserID: u.ID})
		switch {
		case err == nil && existing.AccountID != account:
			return ErrXAccountLinkedElsewhere
		case err == nil:
			if _, err := q.RevokeVerification(ctx, generated.RevokeVerificationParams{ID: existing.ID, Reason: "superseded by re-verification"}); err != nil {
				return err
			}
		case !database.IsNoRows(err):
			return err
		}

		row, err := q.CreateVerification(ctx, generated.CreateVerificationParams{
			AccountID: account, Provider: ProviderX, ProviderUserID: u.ID, Username: u.Username,
			DisplayName: u.Name, ProviderCreatedAt: u.CreatedAt,
		})
		if err != nil {
			return err
		}
		v = toVerification(row)

		rows, err := q.ExtendReservedClaims(ctx, generated.ExtendReservedClaimsParams{
			VerifiedUntil: &renewUntil, VerificationID: row.ID, XUsername: u.Username, AccountID: account, XUserID: u.ID,
		})
		if err != nil {
			return err
		}
		for _, c := range rows {
			if c.MaxID != nil {
				if err := q.SetMaxIDStatus(ctx, generated.SetMaxIDStatusParams{ID: *c.MaxID, Status: "active"}); err != nil {
					return err
				}
			}
			renewed = append(renewed, toClaim(c))
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &account, Action: "verification.x_verified", ResourceType: "account", ResourceID: account.String(),
			Metadata: map[string]any{"x_user_id": u.ID, "x_username": u.Username, "renewed_claims": len(renewed)},
		})
	})
	if err != nil {
		if database.IsUniqueViolation(err, "external_verifications_active_uniq") {
			return Verification{}, nil, ErrXAccountLinkedElsewhere
		}
		return Verification{}, nil, mapErr(err)
	}
	return v, renewed, nil
}

func (r *repository) ListVerifications(ctx context.Context, account uuid.UUID) ([]Verification, error) {
	rows, err := r.q.ListVerificationsByAccount(ctx, account)
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Verification, 0, len(rows))
	for _, row := range rows {
		out = append(out, toVerification(row))
	}
	return out, nil
}

func (r *repository) LatestVerification(ctx context.Context, account uuid.UUID) (*Verification, error) {
	row, err := r.q.GetLatestActiveVerification(ctx, generated.GetLatestActiveVerificationParams{AccountID: account, Provider: ProviderX})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}
		return nil, shared.ErrInternal.WithError(err)
	}
	v := toVerification(row)
	return &v, nil
}

func (r *repository) RevokeVerification(ctx context.Context, id uuid.UUID, account *uuid.UUID, reason string, actor uuid.UUID) error {
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		n, err := q.RevokeVerification(ctx, generated.RevokeVerificationParams{ID: id, Reason: reason, AccountID: account})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrVerificationNotFound
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &actor, Action: "verification.revoked", ResourceType: "verification", ResourceID: id.String(),
			Metadata: map[string]any{"reason": reason},
		})
	})
	return mapErr(err)
}

func (r *repository) GetReserved(ctx context.Context, canonical string) (*Reserved, error) {
	row, err := r.q.GetReservedName(ctx, canonical)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}
		return nil, shared.ErrInternal.WithError(err)
	}
	return toReserved(row), nil
}

func (r *repository) ClaimActive(ctx context.Context, p ClaimParams) (Claim, error) {
	var out Claim
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		res, err := q.LockReservedName(ctx, p.Reserved.CanonicalName)
		if err != nil {
			if database.IsNoRows(err) {
				return maxids.ErrNameTaken // claimed or released in the meantime
			}
			return err
		}
		m, err := maxids.CreateInTx(ctx, q, p.AccountID, res.CanonicalName, maxids.TypeHuman, p.DisplayName, "maxid.reserved_claimed", 0)
		if err != nil {
			return err
		}
		c, err := q.CreateReservedClaim(ctx, generated.CreateReservedClaimParams{
			ReservedNameID: res.ID, CanonicalName: res.CanonicalName, AccountID: p.AccountID, MaxID: &m.ID,
			VerificationID: p.Verification.ID, XUserID: p.Verification.ProviderUserID, XUsername: p.Verification.Username,
			Status: string(ClaimActive), VerifiedUntil: &p.VerifiedUntil,
		})
		if err != nil {
			return err
		}
		if err := q.SetReservedNameStatus(ctx, generated.SetReservedNameStatusParams{ID: res.ID, Status: "claimed"}); err != nil {
			return err
		}
		out = toClaim(c)
		return nil
	})
	return out, mapClaimErr(err)
}

func (r *repository) CreatePendingClaim(ctx context.Context, p ClaimParams) (Claim, error) {
	var out Claim
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		res, err := q.LockReservedName(ctx, p.Reserved.CanonicalName)
		if err != nil {
			if database.IsNoRows(err) {
				return maxids.ErrNameTaken
			}
			return err
		}
		c, err := q.CreateReservedClaim(ctx, generated.CreateReservedClaimParams{
			ReservedNameID: res.ID, CanonicalName: res.CanonicalName, AccountID: p.AccountID,
			VerificationID: p.Verification.ID, XUserID: p.Verification.ProviderUserID, XUsername: p.Verification.Username,
			Status: string(ClaimPendingReview),
		})
		if err != nil {
			return err
		}
		out = toClaim(c)
		acct := p.AccountID
		return audit.Write(ctx, q, audit.Entry{
			Actor: &acct, Action: "maxid.reserved_claim_submitted", ResourceType: "reserved_name", ResourceID: res.ID.String(),
			Metadata: map[string]any{"name": res.CanonicalName, "x_username": p.Verification.Username},
		})
	})
	return out, mapClaimErr(err)
}

func (r *repository) ListClaimsByAccount(ctx context.Context, account uuid.UUID) ([]Claim, error) {
	rows, err := r.q.ListReservedClaimsByAccount(ctx, account)
	return claims(rows, err)
}

func (r *repository) ListClaimsByStatus(ctx context.Context, status ClaimStatus, limit int32) ([]Claim, error) {
	rows, err := r.q.ListReservedClaimsByStatus(ctx, generated.ListReservedClaimsByStatusParams{Status: string(status), Limit: limit})
	return claims(rows, err)
}

func (r *repository) ApproveClaim(ctx context.Context, id, admin uuid.UUID, verifiedUntil time.Time, note string) (Claim, error) {
	var out Claim
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		c, err := q.GetReservedClaimForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if ClaimStatus(c.Status) != ClaimPendingReview {
			return ErrClaimState
		}
		m, err := maxids.CreateInTx(ctx, q, c.AccountID, c.CanonicalName, maxids.TypeHuman, c.CanonicalName, "maxid.reserved_claimed", 0)
		if err != nil {
			return err
		}
		decided, err := q.DecideReservedClaim(ctx, generated.DecideReservedClaimParams{
			ID: id, FromStatus: string(ClaimPendingReview), ToStatus: string(ClaimActive), Note: note,
			DecidedBy: &admin, MaxID: &m.ID, VerifiedUntil: &verifiedUntil,
		})
		if err != nil {
			return err
		}
		if err := q.SetReservedNameStatus(ctx, generated.SetReservedNameStatusParams{ID: c.ReservedNameID, Status: "claimed"}); err != nil {
			return err
		}
		out = toClaim(decided)
		return audit.Write(ctx, q, audit.Entry{
			Actor: &admin, Action: "maxid.reserved_claim_approved", ResourceType: "reserved_claim", ResourceID: id.String(),
			Metadata: map[string]any{"name": c.CanonicalName, "account_id": c.AccountID.String(), "note": note},
		})
	})
	return out, mapClaimErr(err)
}

func (r *repository) DenyClaim(ctx context.Context, id, admin uuid.UUID, note string) (Claim, error) {
	return r.decide(ctx, id, admin, []ClaimStatus{ClaimPendingReview}, ClaimDenied, note, "maxid.reserved_claim_denied", false)
}

func (r *repository) RevokeClaim(ctx context.Context, id, admin uuid.UUID, note string) (Claim, error) {
	return r.decide(ctx, id, admin, []ClaimStatus{ClaimActive, ClaimLapsed}, ClaimRevoked, note, "maxid.reserved_claim_revoked", true)
}

// decide moves a claim between states; lock also locks the claimed ID so it
// stops resolving while a dispute is resolved.
func (r *repository) decide(ctx context.Context, id, admin uuid.UUID, from []ClaimStatus, to ClaimStatus, note, action string, lock bool) (Claim, error) {
	var out Claim
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		c, err := q.GetReservedClaimForUpdate(ctx, id)
		if err != nil {
			return err
		}
		allowed := false
		for _, f := range from {
			if ClaimStatus(c.Status) == f {
				allowed = true
			}
		}
		if !allowed {
			return ErrClaimState
		}
		decided, err := q.DecideReservedClaim(ctx, generated.DecideReservedClaimParams{
			ID: id, FromStatus: c.Status, ToStatus: string(to), Note: note, DecidedBy: &admin,
		})
		if err != nil {
			return err
		}
		if lock && c.MaxID != nil {
			if err := q.SetMaxIDStatus(ctx, generated.SetMaxIDStatusParams{ID: *c.MaxID, Status: "locked"}); err != nil {
				return err
			}
		}
		out = toClaim(decided)
		return audit.Write(ctx, q, audit.Entry{
			Actor: &admin, Action: action, ResourceType: "reserved_claim", ResourceID: id.String(),
			Metadata: map[string]any{"name": c.CanonicalName, "note": note},
		})
	})
	return out, mapClaimErr(err)
}

func (r *repository) LapseOverdue(ctx context.Context, grace time.Duration) ([]Claim, error) {
	var out []Claim
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		rows, err := q.LapseOverdueClaims(ctx, int32(grace.Seconds()))
		if err != nil {
			return err
		}
		for _, c := range rows {
			if c.MaxID != nil {
				if err := q.SetMaxIDStatus(ctx, generated.SetMaxIDStatusParams{ID: *c.MaxID, Status: "locked"}); err != nil {
					return err
				}
			}
			if err := audit.Write(ctx, q, audit.Entry{
				Action: "maxid.reserved_claim_lapsed", ResourceType: "reserved_claim", ResourceID: c.ID.String(),
				Metadata: map[string]any{"name": c.CanonicalName, "x_username": c.XUsername},
			}); err != nil {
				return err
			}
			out = append(out, toClaim(c))
		}
		return nil
	})
	return out, mapErr(err)
}

func toVerification(r generated.ExternalVerification) Verification {
	return Verification{
		ID: r.ID, AccountID: r.AccountID, Provider: r.Provider, ProviderUserID: r.ProviderUserID,
		Username: r.Username, DisplayName: r.DisplayName, VerifiedAt: r.VerifiedAt,
		RevokedAt: r.RevokedAt, RevokeReason: r.RevokeReason,
	}
}

func toClaim(r generated.ReservedNameClaim) Claim {
	return Claim{
		ID: r.ID, ReservedNameID: r.ReservedNameID, CanonicalName: r.CanonicalName, AccountID: r.AccountID,
		MaxID: r.MaxID, XUserID: r.XUserID, XUsername: r.XUsername, Status: ClaimStatus(r.Status),
		VerifiedUntil: r.VerifiedUntil, ReviewNote: r.ReviewNote, DecidedAt: r.DecidedAt, CreatedAt: r.CreatedAt,
	}
}

func toReserved(r generated.ReservedName) *Reserved {
	res := &Reserved{ID: r.ID, CanonicalName: r.CanonicalName, Policy: r.ClaimPolicy}
	if r.XHandle != nil {
		res.XHandle = *r.XHandle
	}
	return res
}

func claims(rows []generated.ReservedNameClaim, err error) ([]Claim, error) {
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Claim, 0, len(rows))
	for _, row := range rows {
		out = append(out, toClaim(row))
	}
	return out, nil
}

func mapClaimErr(err error) error {
	if err == nil {
		return nil
	}
	if database.IsUniqueViolation(err, "reserved_name_claims_live_uniq") {
		return ErrClaimInProgress
	}
	if database.IsNoRows(err) {
		return ErrClaimNotFound
	}
	return maxids.MapCreateErr(err)
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var appErr *shared.AppError
	if errors.As(err, &appErr) {
		return err
	}
	return shared.ErrInternal.WithError(err)
}
