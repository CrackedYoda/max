package market

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/shared"
)

func toSales(rows []generated.MarketSale) []Sale {
	out := make([]Sale, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSale(row, ""))
	}
	return out
}

func (r *repository) ClaimDueSales(ctx context.Context, limit int, lease time.Duration) ([]Sale, error) {
	rows, err := r.q.ClaimDueMarketSales(ctx, generated.ClaimDueMarketSalesParams{LeaseSeconds: lease.Seconds(), RowLimit: int32(limit)})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	return toSales(rows), nil
}

func (r *repository) ClaimSalesPastDeadline(ctx context.Context, cutoff time.Time, limit int, lease time.Duration) ([]Sale, error) {
	rows, err := r.q.ClaimMarketSalesPastTxDeadline(ctx, generated.ClaimMarketSalesPastTxDeadlineParams{
		LeaseSeconds: lease.Seconds(), Cutoff: cutoff, RowLimit: int32(limit),
	})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	return toSales(rows), nil
}

func (r *repository) LeaseSale(ctx context.Context, saleID uuid.UUID, lease time.Duration) (bool, error) {
	n, err := r.q.LeaseMarketSaleCheck(ctx, generated.LeaseMarketSaleCheckParams{LeaseSeconds: lease.Seconds(), ID: saleID})
	if err != nil {
		return false, shared.ErrInternal.WithError(err)
	}
	return n == 1, nil
}

func (r *repository) ExpiredLocks(ctx context.Context, cutoff time.Time, limit int) ([]Sale, error) {
	rows, err := r.q.ListExpiredLockedMarketSales(ctx, generated.ListExpiredLockedMarketSalesParams{Cutoff: cutoff, RowLimit: int32(limit)})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	return toSales(rows), nil
}

func (r *repository) UnlapsedOffers(ctx context.Context, limit int) ([]uuid.UUID, error) {
	ids, err := r.q.ListUnlapsedExpiredOfferSales(ctx, int32(limit))
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	return ids, nil
}

func (r *repository) ExpireListings(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	n := 0
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		rows, err := q.ExpireMarketListings(ctx, generated.ExpireMarketListingsParams{Cutoff: cutoff, RowLimit: int32(limit)})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := audit.Write(ctx, q, audit.Entry{
				Action: "market.listing_expired", ResourceType: "market_listing", ResourceID: row.ID.String(),
				Metadata: map[string]any{"max_id": row.MaxID.String(), "seller": row.SellerAccountID.String()},
			}); err != nil {
				return err
			}
		}
		n = len(rows)
		return nil
	})
	if err != nil {
		return 0, shared.ErrInternal.WithError(err)
	}
	return n, nil
}

func (r *repository) AttachLateTx(ctx context.Context, saleID, buyer uuid.UUID, txHash string) (Sale, error) {
	head, err := r.q.GetMarketSale(ctx, saleID)
	if err != nil {
		return Sale{}, mapErr(err, ErrSaleNotFound)
	}
	var out Sale
	err = database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		// Lock order everywhere: max_ids row, then listing / sale.
		m, err := q.LockMaxIDByID(ctx, head.MaxID)
		if err != nil {
			return err
		}
		sale, err := q.GetMarketSaleForUpdate(ctx, saleID)
		if err != nil {
			return err
		}
		switch {
		case sale.BuyerAccountID != buyer:
			return ErrSaleNotFound
		case sale.TxHash != nil && *sale.TxHash == txHash:
			out = toSale(sale, "")
			return nil
		case sale.TxHash != nil:
			return ErrTxMismatch.WithMessage("a different transaction was already submitted for this sale")
		case sale.Status != SaleExpired:
			return ErrSaleExpired.WithMessage("this sale is " + sale.Status + " and no longer accepts a transaction")
		}
		why := ""
		if m.OwnerAccountID != sale.SellerAccountID {
			why = "the name is no longer the seller's"
		} else if open, err := q.GetOpenMarketSaleByMaxID(ctx, sale.MaxID); err == nil {
			why = "sale " + open.ID.String() + " is open on the name"
		} else if !database.IsNoRows(err) {
			return err
		}
		to, reason, kind := SaleConfirming, "", "late_submitted"
		if why != "" {
			to, reason, kind = SaleManualReview, "transaction submitted after the sale expired, but "+why, "late_payment_review"
		}
		row, err := q.AttachLateMarketSaleTx(ctx, generated.AttachLateMarketSaleTxParams{
			ToStatus: to, Reason: reason, TxHash: &txHash, ID: saleID, Buyer: buyer,
		})
		if err != nil {
			return err
		}
		out = toSale(row, "")
		if to == SaleConfirming {
			// The sale is open again: lock the listing the expiry put back,
			// so completion sells it and a second expiry reactivates it.
			if l, err := q.GetLiveMarketListingByMaxIDForUpdate(ctx, sale.MaxID); err == nil && l.Status == ListingActive {
				if _, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
					ID: l.ID, FromStatus: ListingActive, ToStatus: ListingLocked, Reason: "late payment, sale " + saleID.String(),
				}); err != nil {
					return err
				}
			} else if err != nil && !database.IsNoRows(err) {
				return err
			}
		}
		from := SaleExpired
		if err := event(ctx, q, saleID, kind, &from, to, map[string]any{"tx_hash": txHash, "reason": reason}); err != nil {
			return err
		}
		if to != SaleManualReview {
			return nil
		}
		return audit.Write(ctx, q, audit.Entry{
			Action: "market.sale_manual_review", ResourceType: "market_sale", ResourceID: saleID.String(),
			Metadata: map[string]any{"max_id": sale.MaxID.String(), "reason": reason, "tx_hash": txHash},
		})
	})
	if database.IsUniqueViolation(err, "market_sales_tx_hash_idx") {
		return Sale{}, ErrTxMismatch.WithMessage("this transaction is already attached to another sale")
	}
	if err != nil {
		return Sale{}, mapErr(err, ErrSaleNotFound)
	}
	return out, nil
}
