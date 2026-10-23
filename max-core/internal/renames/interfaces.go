package renames

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
)

type ReserveParams struct {
	MaxID        uuid.UUID
	Owner        uuid.UUID
	OldName      string
	NewName      string
	FeePaymentID uuid.UUID
}

type Repository interface {
	GetOwnedMaxID(ctx context.Context, id, owner uuid.UUID) (MaxIDInfo, error)
	GetReserved(ctx context.Context, canonical string) (*maxids.Reserved, error)
	// Reserve atomically re-checks name availability and cooldown under the
	// name lock and inserts the pending request that holds the new name.
	Reserve(ctx context.Context, p ReserveParams) (Request, error)
	Get(ctx context.Context, id, owner uuid.UUID) (Request, error)
	GetPending(ctx context.Context, maxID uuid.UUID) (*Request, error)
	ListPending(ctx context.Context, limit int32) ([]Request, error)
	// Finalize performs the rename in one transaction: swap the name,
	// quarantine the old one, record history, restart the cooldown.
	Finalize(ctx context.Context, requestID uuid.UUID, cooldown, quarantine time.Duration) (Request, error)
	Close(ctx context.Context, requestID uuid.UUID, status Status, reason string) (Request, error)
	History(ctx context.Context, maxID uuid.UUID) ([]HistoryEntry, error)
}

type Payments interface {
	PrepareFee(ctx context.Context, in payments.FeeInput) (payments.Prepared, error)
	Get(ctx context.Context, caller *uuid.UUID, id uuid.UUID) (payments.Payment, []payments.Event, error)
	Release(ctx context.Context, id uuid.UUID, reason string) (payments.Payment, error)
	Reject(ctx context.Context, id uuid.UUID, reason string) (payments.Payment, error)
	Instructions(p payments.Payment) payments.Instructions
}

type RequestView struct {
	Request      Request
	Payment      payments.Payment
	Instructions *payments.Instructions
	Warnings     []string
}

type Service interface {
	Eligibility(ctx context.Context, owner, maxID uuid.UUID) (Eligibility, error)
	Request(ctx context.Context, owner, maxID uuid.UUID, in RequestInput) (RequestView, error)
	Get(ctx context.Context, owner, maxID, requestID uuid.UUID) (RequestView, error)
	History(ctx context.Context, owner, maxID uuid.UUID) ([]HistoryEntry, error)
	// Sweep reconciles pending requests with their fee payments.
	Sweep(ctx context.Context) (int, error)
}
