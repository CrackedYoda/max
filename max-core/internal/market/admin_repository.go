package market

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

var (
	errAccountNotFound   = shared.ErrNotFound.WithMessage("account not found")
	errAlreadyRestricted = shared.ErrConflict.WithMessage("this account already has an active restriction of this kind; lift it first")
)

// noteTxAfterCancel marks a payment that arrived after an admin cancel.
const noteTxAfterCancel = "tx submitted after admin cancel"

// attachAfterCancel records a transaction submitted for a sale an admin
// cancelled while it was locked. Like a late payment to a name that moved
// on, it goes to manual_review: the buyer may have paid and a human must
// sort out the refund. Runs inside AttachTx's transaction.
func attachAfterCancel(ctx context.Context, q *generated.Queries, saleID, buyer uuid.UUID, txHash string) (generated.MarketSale, error) {
	row, err := q.AttachCancelledMarketSaleTx(ctx, generated.AttachCancelledMarketSaleTxParams{
		ID: saleID, Buyer: buyer, TxHash: &txHash, Reason: noteTxAfterCancel,
	})
	if err != nil {
		return row, err
	}
	from := SaleCancelled
	if err := event(ctx, q, saleID, "tx_after_cancel", &from, SaleManualReview, map[string]any{
		"tx_hash": txHash, "note": noteTxAfterCancel,
	}); err != nil {
		return row, err
	}
	return row, audit.Write(ctx, q, audit.Entry{
		Action: "market.sale_manual_review", ResourceType: "market_sale", ResourceID: saleID.String(),
		Metadata: map[string]any{"max_id": row.MaxID.String(), "reason": noteTxAfterCancel, "tx_hash": txHash},
	})
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *repository) AdminListings(ctx context.Context, f AdminFilter) ([]Listing, error) {
	rows, err := r.q.AdminListMarketListings(ctx, generated.AdminListMarketListingsParams{
		Status: optional(f.Status), AccountID: f.AccountID, Name: optional(f.Name), RowLimit: int32(f.Limit),
	})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Listing, 0, len(rows))
	for _, row := range rows {
		out = append(out, toListing(row.MarketListing, row.CanonicalName))
	}
	return out, nil
}

func (r *repository) AdminOffers(ctx context.Context, f AdminFilter) ([]Offer, error) {
	rows, err := r.q.AdminListMarketOffers(ctx, generated.AdminListMarketOffersParams{
		Status: optional(f.Status), AccountID: f.AccountID, Name: optional(f.Name), RowLimit: int32(f.Limit),
	})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Offer, 0, len(rows))
	for _, row := range rows {
		out = append(out, toOffer(row.MarketOffer, row.CanonicalName))
	}
	return out, nil
}

func (r *repository) AdminSales(ctx context.Context, f AdminFilter) ([]Sale, error) {
	rows, err := r.q.AdminListMarketSales(ctx, generated.AdminListMarketSalesParams{
		Status: optional(f.Status), AccountID: f.AccountID, Name: optional(f.Name), RowLimit: int32(f.Limit),
	})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Sale, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSale(row.MarketSale, row.CanonicalName))
	}
	return out, nil
}

func (r *repository) AdminRestrictions(ctx context.Context, account *uuid.UUID, includeInactive bool, limit int) ([]Restriction, error) {
	rows, err := r.q.AdminListMarketRestrictions(ctx, generated.AdminListMarketRestrictionsParams{
		AccountID: account, IncludeInactive: includeInactive, RowLimit: int32(limit),
	})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Restriction, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRestriction(row))
	}
	return out, nil
}

func (r *repository) AdminDelist(ctx context.Context, actor, listingID uuid.UUID, reason string) (Listing, error) {
	head, err := r.q.GetMarketListing(ctx, listingID)
	if err != nil {
		return Listing{}, mapErr(err, ErrListingNotFound)
	}
	var out Listing
	err = database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockMaxIDByID(ctx, head.MaxID); err != nil {
			return err
		}
		l, err := q.GetMarketListingForUpdate(ctx, listingID)
		if err != nil {
			return err
		}
		switch l.Status {
		case ListingActive:
		case ListingLocked:
			return errDelistLocked
		default:
			return ErrListingNotActive
		}
		row, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
			ID: listingID, FromStatus: ListingActive, ToStatus: ListingAdminDelisted, Reason: reason,
		})
		if err != nil {
			return err
		}
		out = toListing(row, "")
		return audit.Write(ctx, q, audit.Entry{
			Actor: &actor, Action: "market.listing_admin_delisted", ResourceType: "market_listing", ResourceID: listingID.String(),
			Metadata: map[string]any{"max_id": l.MaxID.String(), "seller": l.SellerAccountID.String(), "reason": reason},
		})
	})
	if err != nil {
		return Listing{}, mapErr(err, ErrListingNotFound)
	}
	return out, nil
}

// adminSaleTx runs fn on a sale under the max_ids lock and the sale row lock
// (the lock order used everywhere), passing the locked name and sale.
func (r *repository) adminSaleTx(ctx context.Context, saleID uuid.UUID, fn func(q *generated.Queries, m generated.MaxID, sale generated.MarketSale) error) error {
	head, err := r.q.GetMarketSale(ctx, saleID)
	if err != nil {
		return mapErr(err, ErrSaleNotFound)
	}
	err = database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		m, err := q.LockMaxIDByID(ctx, head.MaxID)
		if err != nil {
			return err
		}
		sale, err := q.GetMarketSaleForUpdate(ctx, saleID)
		if err != nil {
			return err
		}
		return fn(q, m, sale)
	})
	return mapErr(err, ErrSaleNotFound)
}

// adminSaleRecord writes the sale event and the audit entry of an admin
// sale action.
func adminSaleRecord(ctx context.Context, q *generated.Queries, actor uuid.UUID, sale generated.MarketSale, kind, to string, detail map[string]any) error {
	detail["admin_id"] = actor.String()
	from := sale.Status
	if err := event(ctx, q, sale.ID, kind, &from, to, detail); err != nil {
		return err
	}
	meta := map[string]any{"max_id": sale.MaxID.String(), "seller": sale.SellerAccountID.String(),
		"buyer": sale.BuyerAccountID.String(), "from_status": from, "to_status": to}
	for k, v := range detail {
		meta[k] = v
	}
	return audit.Write(ctx, q, audit.Entry{
		Actor: &actor, Action: "market.sale_" + kind, ResourceType: "market_sale", ResourceID: sale.ID.String(), Metadata: meta,
	})
}

// releaseListing returns the name's locked listing to active once no sale
// is open on it any more. A listing whose seller no longer owns the name is
// cancelled instead, since nobody could buy it. It reports the listing's new
// status, or "" when nothing changed.
func releaseListing(ctx context.Context, q *generated.Queries, m generated.MaxID) (string, error) {
	l, err := q.GetLiveMarketListingByMaxIDForUpdate(ctx, m.ID)
	if database.IsNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if l.Status != ListingLocked {
		return "", nil
	}
	if _, err := q.GetOpenMarketSaleByMaxID(ctx, m.ID); err == nil {
		return "", nil // another sale holds the listing
	} else if !database.IsNoRows(err) {
		return "", err
	}
	to, reason := ListingActive, ""
	if l.SellerAccountID != m.OwnerAccountID {
		to, reason = ListingCancelled, "the seller no longer owns the name"
	}
	if _, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
		ID: l.ID, FromStatus: ListingLocked, ToStatus: to, Reason: reason,
	}); err != nil {
		return "", err
	}
	return to, nil
}

func (r *repository) AdminApproveSale(ctx context.Context, actor, saleID uuid.UUID, lockExpiresAt time.Time, reason string) (Sale, error) {
	var out Sale
	err := r.adminSaleTx(ctx, saleID, func(q *generated.Queries, m generated.MaxID, sale generated.MarketSale) error {
		if sale.Status != SalePendingReview {
			return errNotPendingReview
		}
		if m.OwnerAccountID != sale.SellerAccountID {
			return shared.ErrConflict.WithMessage("the seller no longer owns the name; deny the sale instead")
		}
		row, err := q.ApproveMarketSale(ctx, generated.ApproveMarketSaleParams{ID: saleID, LockExpiresAt: &lockExpiresAt})
		if err != nil {
			return err
		}
		// Buy and accept lock the listing when the sale opens, review or
		// not; lock it here too in case it is somehow still active.
		if l, err := q.GetLiveMarketListingByMaxIDForUpdate(ctx, sale.MaxID); err == nil && l.Status == ListingActive {
			if _, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
				ID: l.ID, FromStatus: ListingActive, ToStatus: ListingLocked, Reason: "sale " + saleID.String(),
			}); err != nil {
				return err
			}
		} else if err != nil && !database.IsNoRows(err) {
			return err
		}
		out = toSale(row, "")
		detail := map[string]any{"lock_expires_at": lockExpiresAt.UTC().Format(time.RFC3339)}
		if reason != "" {
			detail["reason"] = reason
		}
		return adminSaleRecord(ctx, q, actor, sale, "approved", SaleLocked, detail)
	})
	if err != nil {
		return Sale{}, err
	}
	return out, nil
}

func (r *repository) AdminDenySale(ctx context.Context, actor, saleID uuid.UUID, reason string) (Sale, error) {
	var out Sale
	err := r.adminSaleTx(ctx, saleID, func(q *generated.Queries, m generated.MaxID, sale generated.MarketSale) error {
		if sale.Status != SalePendingReview {
			return errNotPendingReview
		}
		row, err := q.TransitionMarketSale(ctx, generated.TransitionMarketSaleParams{
			ID: saleID, FromStatus: SalePendingReview, ToStatus: SaleCancelled, Reason: adminDenyNote(reason),
		})
		if err != nil {
			return err
		}
		detail := map[string]any{"reason": reason}
		if sale.OfferID != nil {
			// Declined, not lapsed: the buyer earns no strike.
			if _, err := q.AdminDeclineMarketOffer(ctx, generated.AdminDeclineMarketOfferParams{
				ID: *sale.OfferID, Note: adminDenyNote(reason),
			}); err == nil {
				detail["offer"] = OfferDeclined
			} else if !database.IsNoRows(err) {
				return err
			}
		}
		if to, err := releaseListing(ctx, q, m); err != nil {
			return err
		} else if to != "" {
			detail["listing"] = to
		}
		out = toSale(row, "")
		return adminSaleRecord(ctx, q, actor, sale, "denied", SaleCancelled, detail)
	})
	if err != nil {
		return Sale{}, err
	}
	return out, nil
}

func (r *repository) AdminCancelSale(ctx context.Context, actor, saleID uuid.UUID, reason string) (Sale, error) {
	var out Sale
	err := r.adminSaleTx(ctx, saleID, func(q *generated.Queries, m generated.MaxID, sale generated.MarketSale) error {
		if err := cancellable(toSale(sale, "")); err != nil {
			return err
		}
		row, err := q.TransitionMarketSale(ctx, generated.TransitionMarketSaleParams{
			ID: saleID, FromStatus: sale.Status, ToStatus: SaleCancelled, Reason: "cancelled by admin: " + reason,
		})
		if err != nil {
			return err
		}
		detail := map[string]any{"reason": reason}
		if sale.TxHash != nil {
			detail["tx_hash"] = *sale.TxHash
			detail["refund"] = "refund handled off-platform"
		}
		if sale.OfferID != nil {
			// Expired, not lapsed: the buyer earns no strike.
			if _, err := q.TransitionMarketOffer(ctx, generated.TransitionMarketOfferParams{
				ID: *sale.OfferID, FromStatuses: []string{OfferAccepted}, ToStatus: OfferExpired,
			}); err == nil {
				detail["offer"] = OfferExpired
			} else if !database.IsNoRows(err) {
				return err
			}
		}
		if to, err := releaseListing(ctx, q, m); err != nil {
			return err
		} else if to != "" {
			detail["listing"] = to
		}
		out = toSale(row, "")
		return adminSaleRecord(ctx, q, actor, sale, "cancelled", SaleCancelled, detail)
	})
	if err != nil {
		return Sale{}, err
	}
	return out, nil
}

func (r *repository) AdminReverseSale(ctx context.Context, actor, saleID uuid.UUID, reason string, hold time.Duration) (Sale, error) {
	var out Sale
	err := r.adminSaleTx(ctx, saleID, func(q *generated.Queries, m generated.MaxID, sale generated.MarketSale) error {
		if sale.Status != SaleCompleted {
			return errNotReversible
		}
		if m.OwnerAccountID != sale.BuyerAccountID {
			return errBuyerNotOwner
		}
		if err := maxids.EnsureNoOpenSale(ctx, q, sale.MaxID); err != nil {
			return err
		}
		detail := map[string]any{"reason": reason, "money": "handled off-platform"}
		// A listing the buyer put up since closes: the name is no longer theirs.
		if l, err := q.GetLiveMarketListingByMaxIDForUpdate(ctx, sale.MaxID); err == nil {
			if _, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
				ID: l.ID, FromStatus: l.Status, ToStatus: ListingCancelled, Reason: "ownership reversed by admin",
			}); err != nil {
				return err
			}
			detail["cancelled_listing_id"] = l.ID.String()
		} else if !database.IsNoRows(err) {
			return err
		}
		ref := sale.ID
		terr := maxids.TransferOwnership(ctx, q, maxids.TransferParams{
			MaxID: sale.MaxID, From: sale.BuyerAccountID, To: sale.SellerAccountID, SaleID: &ref,
			DestinationWalletID: uuid.Nil, HoldPeriod: hold, Reason: "market sale reversed by admin",
		})
		if errors.Is(terr, maxids.ErrTransferOwnerMismatch) {
			return errBuyerNotOwner
		}
		if terr != nil {
			return terr
		}
		// Offers made while the buyer owned the name are void, as after a sale.
		if err := q.SupersedeMarketOffers(ctx, generated.SupersedeMarketOffersParams{MaxID: sale.MaxID}); err != nil {
			return err
		}
		row, err := q.TransitionMarketSale(ctx, generated.TransitionMarketSaleParams{
			ID: saleID, FromStatus: SaleCompleted, ToStatus: SaleReversed, Reason: "reversed by admin: " + reason,
		})
		if err != nil {
			return err
		}
		out = toSale(row, "")
		return adminSaleRecord(ctx, q, actor, sale, "reversed", SaleReversed, detail)
	})
	if err != nil {
		return Sale{}, err
	}
	return out, nil
}

func (r *repository) AdminRestrict(ctx context.Context, actor, account uuid.UUID, in NewRestriction) (Restriction, error) {
	var out Restriction
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockMarketRestrictionKind(ctx, generated.LockMarketRestrictionKindParams{AccountID: account.String(), Kind: in.Kind}); err != nil {
			return err
		}
		active, err := q.HasActiveMarketRestriction(ctx, generated.HasActiveMarketRestrictionParams{AccountID: account, Kind: in.Kind})
		if err != nil {
			return err
		}
		if active {
			return errAlreadyRestricted
		}
		out, err = restrict(ctx, q, account, in, &actor)
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return Restriction{}, errAccountNotFound
	}
	if err != nil {
		return Restriction{}, mapErr(err, errAccountNotFound)
	}
	return out, nil
}

func (r *repository) LiftRestriction(ctx context.Context, actor, id uuid.UUID, reason string) (Restriction, error) {
	var out Restriction
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.LiftMarketRestriction(ctx, id)
		if database.IsNoRows(err) {
			return ErrRestrictionNotFound.WithMessage("no restriction with this id is in force")
		}
		if err != nil {
			return err
		}
		out = toRestriction(row)
		meta := map[string]any{"account_id": row.AccountID.String(), "kind": row.Kind}
		if reason != "" {
			meta["reason"] = reason
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &actor, Action: "market.restriction_lifted", ResourceType: "market_restriction", ResourceID: id.String(), Metadata: meta,
		})
	})
	if err != nil {
		return Restriction{}, mapErr(err, ErrRestrictionNotFound)
	}
	return out, nil
}
