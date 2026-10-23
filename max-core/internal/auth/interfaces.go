package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	CreateChallenge(ctx context.Context, c Challenge) error
	// ConsumeChallenge atomically marks a live challenge used; ErrChallengeInvalid otherwise.
	ConsumeChallenge(ctx context.Context, nonce string) (Challenge, error)
	// LoginWithWallet returns the account owning the wallet, creating both if new.
	LoginWithWallet(ctx context.Context, proof WalletProof) (Account, Wallet, bool, error)
	LinkWallet(ctx context.Context, accountID uuid.UUID, proof WalletProof) (Wallet, error)
	GetAccount(ctx context.Context, id uuid.UUID) (Account, error)
	CreateSession(ctx context.Context, accountID uuid.UUID, tokenHash []byte, ip, userAgent string, expiresAt time.Time) (Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, string, error)
	RevokeSession(ctx context.Context, id uuid.UUID) error
}

type Service interface {
	CreateChallenge(ctx context.Context, in ChallengeInput) (Challenge, error)
	Verify(ctx context.Context, in VerifyInput) (VerifyResult, error)
	Authenticate(ctx context.Context, token string) (Session, error)
	Logout(ctx context.Context, sessionID uuid.UUID) error
	Me(ctx context.Context, accountID uuid.UUID) (Account, error)
}
