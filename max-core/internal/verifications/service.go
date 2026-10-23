package verifications

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

type service struct {
	repo Repository
	x    XClient
	cfg  Config
	log  *zap.Logger
	now  func() time.Time
}

var _ Service = (*service)(nil)

func NewService(repo Repository, x XClient, cfg Config, log *zap.Logger) Service {
	return &service{repo: repo, x: x, cfg: cfg, log: log, now: time.Now}
}

func (s *service) ResultRedirect() string { return s.cfg.ResultRedirect }

func (s *service) StartX(ctx context.Context, account, session uuid.UUID) (string, time.Time, error) {
	if !s.x.Enabled() {
		return "", time.Time{}, ErrXDisabled
	}
	if session == uuid.Nil {
		return "", time.Time{}, ErrSessionRequired
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, shared.ErrInternal.WithError(err)
	}
	state := base64.RawURLEncoding.EncodeToString(raw)
	verifier, challenge, err := newPKCE()
	if err != nil {
		return "", time.Time{}, shared.ErrInternal.WithError(err)
	}
	expires := s.now().Add(s.cfg.StateTTL)
	if err := s.repo.CreateState(ctx, hashState(state), account, session, verifier, expires); err != nil {
		return "", time.Time{}, err
	}
	return s.x.AuthorizeURL(state, challenge), expires, nil
}

func (s *service) CompleteX(ctx context.Context, account, session *uuid.UUID, state, code, oauthError string) (Verification, []Claim, error) {
	if !s.x.Enabled() {
		return Verification{}, nil, ErrXDisabled
	}
	if state == "" {
		return Verification{}, nil, ErrOAuthStateInvalid
	}
	// Consume first so every state is single-use, even when the user declined.
	st, err := s.repo.ConsumeState(ctx, hashState(state))
	if err != nil {
		return Verification{}, nil, err
	}
	if oauthError != "" {
		return Verification{}, nil, ErrOAuthDenied
	}
	// Without this, an attacker could start a flow on their own account and
	// trick a victim signed in to X as @brand into approving it.
	if account == nil || session == nil || *account != st.AccountID || *session != st.SessionID {
		return Verification{}, nil, ErrOAuthSessionMismatch
	}
	if code == "" {
		return Verification{}, nil, ErrOAuthStateInvalid
	}

	token, err := s.x.Exchange(ctx, code, st.Verifier)
	if err != nil {
		return Verification{}, nil, ErrXProvider.WithError(err)
	}
	user, err := s.x.Me(ctx, token)
	// MAX needs no ongoing access to the X account.
	if rerr := s.x.Revoke(ctx, token); rerr != nil {
		s.log.Warn("revoke X token", zap.Error(rerr))
	}
	if err != nil {
		return Verification{}, nil, ErrXProvider.WithError(err)
	}
	return s.repo.SaveVerification(ctx, st.AccountID, user, s.now().Add(s.cfg.ReverifyInterval))
}

func (s *service) ListVerifications(ctx context.Context, account uuid.UUID) ([]Verification, error) {
	return s.repo.ListVerifications(ctx, account)
}

func (s *service) RevokeMine(ctx context.Context, account, id uuid.UUID) error {
	return s.repo.RevokeVerification(ctx, id, &account, "revoked by owner", account)
}

func (s *service) ClaimReserved(ctx context.Context, account uuid.UUID, name, displayName string) (ClaimResult, error) {
	canonical, err := maxids.Normalize(name)
	if err != nil {
		return ClaimResult{}, maxids.ErrInvalidName.WithMessage(err.Error())
	}
	reserved, err := s.repo.GetReserved(ctx, canonical)
	if err != nil {
		return ClaimResult{}, err
	}
	if reserved == nil {
		return ClaimResult{}, ErrNotReserved
	}
	if reserved.Policy == "blocked" || reserved.XHandle == "" {
		return ClaimResult{}, maxids.ErrNameReserved
	}

	v, err := s.repo.LatestVerification(ctx, account)
	if err != nil {
		return ClaimResult{}, err
	}
	if v == nil {
		return ClaimResult{}, ErrVerificationRequired.WithMessage("verify the X account @" + reserved.XHandle + " first")
	}
	if !strings.EqualFold(v.Username, reserved.XHandle) {
		return ClaimResult{}, ErrXHandleMismatch.WithMessage("this name requires X account @" + reserved.XHandle + "; you verified @" + v.Username)
	}
	if s.now().Sub(v.VerifiedAt) > s.cfg.ClaimFreshness {
		return ClaimResult{}, ErrVerificationStale
	}

	display := strings.TrimSpace(displayName)
	if display == "" {
		display = canonical
	}
	p := ClaimParams{Reserved: *reserved, AccountID: account, Verification: *v, DisplayName: display,
		VerifiedUntil: s.now().Add(s.cfg.ReverifyInterval)}

	if reserved.Policy == "manual_review" {
		c, err := s.repo.CreatePendingClaim(ctx, p)
		if err != nil {
			return ClaimResult{}, err
		}
		return ClaimResult{Claim: c, Handle: maxids.Handle(canonical), Pending: true}, nil
	}
	c, err := s.repo.ClaimActive(ctx, p)
	if err != nil {
		return ClaimResult{}, err
	}
	return ClaimResult{Claim: c, Handle: maxids.Handle(canonical)}, nil
}

func (s *service) ListClaims(ctx context.Context, account uuid.UUID) ([]Claim, error) {
	return s.repo.ListClaimsByAccount(ctx, account)
}

func (s *service) AdminListClaims(ctx context.Context, status ClaimStatus) ([]Claim, error) {
	if status == "" {
		status = ClaimPendingReview
	}
	return s.repo.ListClaimsByStatus(ctx, status, 200)
}

func (s *service) AdminApprove(ctx context.Context, admin, id uuid.UUID, note string) (Claim, error) {
	return s.repo.ApproveClaim(ctx, id, admin, s.now().Add(s.cfg.ReverifyInterval), truncate(note))
}

func (s *service) AdminDeny(ctx context.Context, admin, id uuid.UUID, note string) (Claim, error) {
	return s.repo.DenyClaim(ctx, id, admin, truncate(note))
}

func (s *service) AdminRevokeClaim(ctx context.Context, admin, id uuid.UUID, note string) (Claim, error) {
	return s.repo.RevokeClaim(ctx, id, admin, truncate(note))
}

func (s *service) AdminRevokeVerification(ctx context.Context, admin, id uuid.UUID, note string) error {
	return s.repo.RevokeVerification(ctx, id, nil, "revoked by administrator: "+truncate(note), admin)
}

func (s *service) Sweep(ctx context.Context) (int, error) {
	lapsed, err := s.repo.LapseOverdue(ctx, s.cfg.ReverifyGrace)
	return len(lapsed), err
}

// hashState keeps raw OAuth states out of the database; they appear in URLs.
func hashState(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}

func newPKCE() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 280 {
		return s[:280]
	}
	return s
}
