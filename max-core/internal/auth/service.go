package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

type Config struct {
	// Domain and URI go into sign-in messages by default.
	Domain string
	URI    string
	// Origins are sites (scheme://host[:port]) whose own domain goes into the
	// message instead, so wallets see the site the user is actually on.
	Origins       []string
	ChallengeTTL  time.Duration
	SessionTTL    time.Duration
	SessionSecret string
}

type service struct {
	repo Repository
	cfg  Config
	now  func() time.Time
}

var _ Service = (*service)(nil)

func NewService(repo Repository, cfg Config) Service {
	return &service{repo: repo, cfg: cfg, now: time.Now}
}

func (s *service) CreateChallenge(ctx context.Context, in ChallengeInput) (Challenge, error) {
	if !in.Purpose.Valid() {
		return Challenge{}, ErrInvalidPurpose
	}
	ns, ok := chains.ParseNamespace(in.Namespace)
	if !ok || !chains.IsLinkable(ns) {
		return Challenge{}, ErrUnsupportedChain
	}
	addr, err := chains.NormalizeAddress(ns, in.Address)
	if err != nil {
		return Challenge{}, ErrInvalidAddress.WithMessage(err.Error())
	}
	if (in.Purpose == PurposeLink || in.Purpose == PurposeClaim) && in.Caller == nil {
		return Challenge{}, shared.ErrUnauthorized
	}
	var claimName string
	if in.Purpose == PurposeClaim {
		if claimName, err = maxids.Normalize(in.Name); err != nil {
			return Challenge{}, ErrClaimChallengeName.WithMessage(err.Error())
		}
		owner, err := s.repo.WalletOwner(ctx, ns, addr)
		if err != nil {
			return Challenge{}, err
		}
		if owner != *in.Caller {
			return Challenge{}, ErrClaimWalletNotOwned
		}
	}

	nonce, err := randomHex(16)
	if err != nil {
		return Challenge{}, shared.ErrInternal.WithError(err)
	}
	now := s.now().UTC().Truncate(time.Second)
	c := Challenge{
		Nonce:     nonce,
		Purpose:   in.Purpose,
		Namespace: ns,
		Address:   addr,
		ExpiresAt: now.Add(s.cfg.ChallengeTTL),
	}
	if in.Purpose == PurposeLink || in.Purpose == PurposeClaim {
		c.AccountID = in.Caller
	}
	c.ClaimName = claimName
	domain, uri := s.signInSite(in.Origin)
	c.Message = buildMessage(messageParams{
		Domain: domain, URI: uri, Namespace: ns, Address: addr,
		Purpose: in.Purpose, ClaimName: claimName, Nonce: nonce, IssuedAt: now, ExpiresAt: c.ExpiresAt,
	})

	if err := s.repo.CreateChallenge(ctx, c); err != nil {
		return Challenge{}, err
	}
	return c, nil
}

// signInSite returns the domain and URI for a sign-in message: the requesting
// site when it is one of cfg.Origins, otherwise the configured default. The
// message is stored with the challenge, so verification needs no change.
func (s *service) signInSite(origin string) (domain, uri string) {
	if origin != "" && slices.Contains(s.cfg.Origins, origin) {
		if u, err := url.Parse(origin); err == nil && u.Host != "" {
			return u.Host, origin
		}
	}
	return s.cfg.Domain, s.cfg.URI
}

func (s *service) Verify(ctx context.Context, in VerifyInput) (VerifyResult, error) {
	// Consuming before verifying means a bad signature still burns the nonce,
	// so each challenge gets exactly one attempt.
	c, err := s.repo.ConsumeChallenge(ctx, in.Nonce)
	if err != nil {
		return VerifyResult{}, err
	}
	if err := chains.VerifySignature(c.Namespace, c.Address, c.Message, in.Signature); err != nil {
		return VerifyResult{}, ErrSignatureInvalid.WithError(err)
	}

	proof := WalletProof{Namespace: c.Namespace, Address: c.Address, Message: c.Message, Signature: in.Signature}

	switch c.Purpose {
	case PurposeClaim:
		// Claim challenges are only spent by VerifyClaim.
		return VerifyResult{}, ErrChallengeInvalid
	case PurposeLink:
		if c.AccountID == nil || in.Caller == nil || *c.AccountID != *in.Caller {
			return VerifyResult{}, ErrChallengeInvalid
		}
		acct, err := s.repo.GetAccount(ctx, *c.AccountID)
		if err != nil {
			return VerifyResult{}, err
		}
		if acct.Status != "active" {
			return VerifyResult{}, ErrAccountSuspended
		}
		w, err := s.repo.LinkWallet(ctx, acct.ID, proof)
		if err != nil {
			return VerifyResult{}, err
		}
		return VerifyResult{Purpose: PurposeLink, Account: acct, Wallet: w}, nil

	case PurposeLogin:
		acct, w, created, err := s.repo.LoginWithWallet(ctx, proof)
		if err != nil {
			return VerifyResult{}, err
		}
		if acct.Status != "active" {
			return VerifyResult{}, ErrAccountSuspended
		}
		// An agent's wallet must never yield a session: sessions are unscoped
		// and would bypass the agent's credential scopes and spending policy.
		if acct.Type == "agent" {
			return VerifyResult{}, ErrAgentLogin
		}
		token, err := randomToken()
		if err != nil {
			return VerifyResult{}, shared.ErrInternal.WithError(err)
		}
		sess, err := s.repo.CreateSession(ctx, acct.ID, s.hashToken(token), in.IP, in.UserAgent, s.now().Add(s.cfg.SessionTTL))
		if err != nil {
			return VerifyResult{}, err
		}
		return VerifyResult{
			Purpose: PurposeLogin, Account: acct, Wallet: w, AccountCreated: created,
			SessionToken: token, Session: &sess,
		}, nil
	}
	return VerifyResult{}, ErrChallengeInvalid
}

func (s *service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrSessionInvalid
	}
	sess, status, err := s.repo.GetSessionByTokenHash(ctx, s.hashToken(token))
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			return Session{}, ErrSessionInvalid
		}
		return Session{}, err
	}
	if status != "active" {
		return Session{}, ErrAccountSuspended
	}
	return sess, nil
}

func (s *service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.repo.RevokeSession(ctx, sessionID)
}

func (s *service) Me(ctx context.Context, accountID uuid.UUID) (MeResult, error) {
	acct, err := s.repo.GetAccount(ctx, accountID)
	if err != nil {
		return MeResult{}, err
	}
	def, primary, err := s.repo.GetAccountIDs(ctx, accountID)
	if err != nil {
		return MeResult{}, err
	}
	return MeResult{Account: acct, DefaultMaxID: def, PrimaryMaxID: primary}, nil
}

// hashToken uses HMAC rather than a bare hash so a leaked sessions table
// cannot be used to forge lookups without the server secret.
func (s *service) hashToken(token string) []byte {
	m := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	m.Write([]byte(token))
	return m.Sum(nil)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *service) VerifyClaim(ctx context.Context, p ClaimProof) error {
	c, err := s.repo.ConsumeChallenge(ctx, p.Nonce)
	if err != nil {
		return err
	}
	if c.Purpose != PurposeClaim || c.AccountID == nil || *c.AccountID != p.Caller || c.ClaimName != p.Name {
		return ErrChallengeInvalid
	}
	if err := chains.VerifySignature(c.Namespace, c.Address, c.Message, p.Signature); err != nil {
		return ErrSignatureInvalid.WithError(err)
	}
	// The wallet may have been unlinked between the challenge and the claim.
	owner, err := s.repo.WalletOwner(ctx, c.Namespace, c.Address)
	if err != nil {
		return err
	}
	if owner != p.Caller {
		return ErrClaimWalletNotOwned
	}
	return nil
}
