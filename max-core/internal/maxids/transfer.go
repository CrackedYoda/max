package maxids

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/database"
)

// DefaultHoldPeriod is used when no market hold period is configured.
const DefaultHoldPeriod = 72 * time.Hour

func holdSeconds(d time.Duration) int32 {
	if d <= 0 {
		d = DefaultHoldPeriod
	}
	return int32(d.Seconds())
}

// TransferParams describes one ownership move.
type TransferParams struct {
	MaxID, From, To uuid.UUID
	// SaleID links the ownership event to a market sale; nil for non-sale moves.
	SaleID *uuid.UUID
	// DestinationWalletID is the new owner's wallet to point the ID at, in its
	// own namespace. uuid.Nil sets no destination.
	DestinationWalletID uuid.UUID
	// HoldPeriod before the ID is listable again; zero means DefaultHoldPeriod.
	HoldPeriod time.Duration
	// Reason is for the caller's audit entry; ownership events do not store it.
	Reason string
}

// TransferOwnership moves a MAX ID to a new owner and resets everything the
// previous owner configured (and aligns max_ids.type with the new owner's
// account type), so the buyer never inherits a payout route they
// did not choose. It runs on the caller's transaction, and the caller must
// already hold SELECT ... FOR UPDATE on the max_ids row (LockMaxIDByID).
// max_id_history and name_quarantine are left untouched. The caller writes
// the audit entry for the surrounding sale or reversal.
func TransferOwnership(ctx context.Context, q *generated.Queries, p TransferParams) error {
	if p.From == p.To {
		return ErrTransferSameAccount
	}
	if row, err := q.LockMaxIDByID(ctx, p.MaxID); err == nil && row.IsDefault {
		return ErrDefaultNotTransferable
	} else if err != nil && !database.IsNoRows(err) {
		return err
	}
	// A pending rename would finalize against the new owner's ID; the caller
	// must resolve it first. Checked inside the caller's transaction.
	if _, err := q.GetPendingRenameForMaxID(ctx, p.MaxID); err == nil {
		return ErrRenamePending
	} else if !database.IsNoRows(err) {
		return err
	}
	if p.DestinationWalletID != uuid.Nil {
		w, err := q.GetOwnedActiveWallet(ctx, generated.GetOwnedActiveWalletParams{ID: p.DestinationWalletID, AccountID: p.To})
		if err != nil {
			if database.IsNoRows(err) {
				return ErrWalletNotFound
			}
			return err
		}
		return transferRows(ctx, q, p, w.ChainNamespace)
	}
	return transferRows(ctx, q, p, "")
}

func transferRows(ctx context.Context, q *generated.Queries, p TransferParams, namespace string) error {
	if _, err := q.TransferMaxIDOwner(ctx, generated.TransferMaxIDOwnerParams{
		ID: p.MaxID, FromOwner: p.From, ToOwner: p.To, HoldSeconds: holdSeconds(p.HoldPeriod),
	}); err != nil {
		if database.IsNoRows(err) {
			return ErrTransferOwnerMismatch
		}
		return err
	}
	if err := q.DeleteAllDestinations(ctx, p.MaxID); err != nil {
		return err
	}
	if p.DestinationWalletID != uuid.Nil {
		if err := q.UpsertDestination(ctx, generated.UpsertDestinationParams{
			MaxID: p.MaxID, ChainNamespace: namespace, WalletID: &p.DestinationWalletID,
		}); err != nil {
			return err
		}
	}
	if _, err := q.DeleteSettlementPreference(ctx, p.MaxID); err != nil {
		return err
	}
	return q.CreateTransferEvent(ctx, generated.CreateTransferEventParams{
		MaxID: p.MaxID, PreviousOwner: &p.From, NextOwner: p.To, SaleID: p.SaleID,
	})
}
