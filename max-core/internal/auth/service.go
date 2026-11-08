package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

type Config struct {
	Domain        string
	URI           string
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
	if in.Purpose == PurposeLink && in.Caller == nil {
		return Challenge{}, shared.ErrUnauthorized
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
	if in.Purpose == PurposeLink {
		c.AccountID = in.Caller
	}
	c.Message = buildMessage(messageParams{
		Domain: s.cfg.Domain, URI: s.cfg.URI, Namespace: ns, Address: addr,
		Purpose: in.Purpose, Nonce: nonce, IssuedAt: now, ExpiresAt: c.ExpiresAt,
	})

	if err := s.repo.CreateChallenge(ctx, c); err != nil {
		return Challenge{}, err
	}
	return c, nil
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
