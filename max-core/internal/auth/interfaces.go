package auth

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
)

type Repository interface {
	CreateChallenge(ctx context.Context, c Challenge) error
	// ConsumeChallenge atomically marks a live challenge used; ErrChallengeInvalid otherwise.
	ConsumeChallenge(ctx context.Context, nonce string) (Challenge, error)
	// WalletOwner is the account an active wallet belongs to; uuid.Nil if none.
	WalletOwner(ctx context.Context, ns chains.Namespace, address string) (uuid.UUID, error)
	// LoginWithWallet returns the account owning the wallet, creating both if new.
	LoginWithWallet(ctx context.Context, proof WalletProof) (Account, Wallet, bool, error)
	LinkWallet(ctx context.Context, accountID uuid.UUID, proof WalletProof) (Wallet, error)
	GetAccount(ctx context.Context, id uuid.UUID) (Account, error)
	// GetAccountIDs returns the account's default ID and its primary ID: the
	// oldest active non-default ID, else the default. Nil when it has no IDs.
	GetAccountIDs(ctx context.Context, id uuid.UUID) (def, primary *IDRef, err error)
	CreateSession(ctx context.Context, accountID uuid.UUID, tokenHash []byte, ip, userAgent string, expiresAt time.Time) (Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, string, error)
	RevokeSession(ctx context.Context, id uuid.UUID) error
}

type Service interface {
	CreateChallenge(ctx context.Context, in ChallengeInput) (Challenge, error)
	Verify(ctx context.Context, in VerifyInput) (VerifyResult, error)
	// VerifyClaim consumes a claim challenge and checks it was signed, for
	// exactly this name, by a wallet the caller still owns.
	VerifyClaim(ctx context.Context, p ClaimProof) error
	Authenticate(ctx context.Context, token string) (Session, error)
	Logout(ctx context.Context, sessionID uuid.UUID) error
	Me(ctx context.Context, accountID uuid.UUID) (MeResult, error)
}
