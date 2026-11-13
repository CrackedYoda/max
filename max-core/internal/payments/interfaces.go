package payments

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/resolver"
)

type Repository interface {
	// Create inserts the intent and its "created" event. Returns
	// errDuplicateIdempotency if (sender, key) already exists.
	Create(ctx context.Context, p Payment) (Payment, error)
	GetByIdempotencyKey(ctx context.Context, senderAddress, key string) (Payment, error)
	Get(ctx context.Context, id uuid.UUID) (Payment, error)
	Events(ctx context.Context, id uuid.UUID) ([]Event, error)
	ListBySender(ctx context.Context, accountID uuid.UUID, limit int32) ([]Payment, error)
	ListReceived(ctx context.Context, accountID uuid.UUID, limit int32) ([]Payment, error)
	// Apply performs a compare-and-swap on status and records the event.
	// Returns ErrStaleTransition if the status moved, ErrTxAlreadyUsed if the
	// hash is attached elsewhere.
	Apply(ctx context.Context, id uuid.UUID, t Transition) (Payment, error)
	Reschedule(ctx context.Context, id uuid.UUID, status Status, in time.Duration, confirmations int64) error
	LeaseDue(ctx context.Context, batch int32, lease time.Duration) ([]Payment, error)
	ExpireAwaiting(ctx context.Context) ([]uuid.UUID, error)
	// MarkDepositConfirmed records origin-chain finality of a cross-chain deposit, once.
	MarkDepositConfirmed(ctx context.Context, id uuid.UUID, status Status) error
	UpdateRouteProgress(ctx context.Context, id uuid.UUID, p RouteProgress) error
}

// Resolver is satisfied by resolver.Service.
type Resolver interface {
	Resolve(ctx context.Context, name, chain, asset string) (resolver.Resolution, error)
	Settlement(ctx context.Context, name string) (*resolver.Settlement, error)
}

type TokenInfo struct {
	Decimals     int
	TokenProgram string
}

// Gateway talks to chain RPCs.
type Gateway interface {
	Configured(chainKey string) bool
	TokenInfo(ctx context.Context, chain chains.Chain, tokenAddress string) (TokenInfo, error)
	AccountExists(ctx context.Context, chain chains.Chain, address string) (bool, error)
	Observe(ctx context.Context, p Payment) (Observation, error)
}

type Service interface {
	Prepare(ctx context.Context, in PrepareInput) (Prepared, error)
	// ReceivableAssets lists what a cross-chain payment can deliver: every
	// asset the routing provider supports on a MAX mainnet chain.
	ReceivableAssets(ctx context.Context) ([]ReceivableAsset, error)
	// PrepareFee creates a direct payment to a MAX treasury address.
	PrepareFee(ctx context.Context, in FeeInput) (Prepared, error)
	Submit(ctx context.Context, in SubmitInput) (Payment, error)
	Get(ctx context.Context, caller *uuid.UUID, id uuid.UUID) (Payment, []Event, error)
	List(ctx context.Context, caller uuid.UUID, direction string, limit int32) ([]Payment, error)
	// Process advances one submitted payment from on-chain evidence.
	Process(ctx context.Context, p Payment) error
	ExpireStale(ctx context.Context) (int, error)
	Instructions(p Payment) Instructions
	// Release moves a held intent to signable (AWAITING_SENDER_SIGNATURE or QUOTED).
	Release(ctx context.Context, id uuid.UUID, reason string) (Payment, error)
	// Reject fails a held intent.
	Reject(ctx context.Context, id uuid.UUID, reason string) (Payment, error)
}
