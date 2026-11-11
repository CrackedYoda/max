package market

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

var openOfferStatuses = []string{OfferLive, OfferUnfunded}

func (r *repository) MaxIDByName(ctx context.Context, name string) (uuid.UUID, error) {
	id, err := r.q.GetMaxIDIDByCanonicalName(ctx, name)
	if err != nil {
		return uuid.Nil, mapErr(err, maxids.ErrMaxIDNotFound)
	}
	return id, nil
}

func (r *repository) ActiveRestriction(ctx context.Context, account uuid.UUID, kind string) (*Restriction, error) {
	row, err := r.q.GetActiveMarketRestriction(ctx, generated.GetActiveMarketRestrictionParams{AccountID: account, Kind: kind})
	if database.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := toRestriction(row)
	return &out, nil
}

func (r *repository) Restrict(ctx context.Context, account uuid.UUID, kind string, until *time.Time, reason string, by *uuid.UUID) error {
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := restrict(ctx, r.q.WithTx(tx), account, NewRestriction{Kind: kind, Until: until, Reason: reason}, by)
		return err
	})
	return mapErr(err, shared.ErrNotFound)
}

func restrict(ctx context.Context, q *generated.Queries, account uuid.UUID, in NewRestriction, by *uuid.UUID) (Restriction, error) {
	row, err := q.CreateMarketRestriction(ctx, generated.CreateMarketRestrictionParams{
		AccountID: account, Kind: in.Kind, Until: in.Until, Reason: in.Reason, CreatedBy: by,
	})
	if err != nil {
		return Restriction{}, err
	}
	meta := map[string]any{"account_id": account.String(), "kind": in.Kind, "reason": in.Reason}
	if in.Until != nil {
		meta["until"] = in.Until.UTC().Format(time.RFC3339)
	}
	return toRestriction(row), audit.Write(ctx, q, audit.Entry{
		Actor: by, Action: "market.restriction_created", ResourceType: "market_restriction", ResourceID: row.ID.String(), Metadata: meta,
	})
}

func (r *repository) OpenOfferCount(ctx context.Context, buyer uuid.UUID) (int, error) {
	n, err := r.q.CountOpenMarketOffersByBuyer(ctx, buyer)
	if err != nil {
		return 0, shared.ErrInternal.WithError(err)
	}
	return int(n), nil
}

func (r *repository) CreateOffer(ctx context.Context, in NewOffer, maxOpen int) (Offer, error) {
	var out Offer
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockMarketOfferBuyer(ctx, in.Buyer.String()); err != nil {
			return err
		}
		n, err := q.CountOpenMarketOffersByBuyer(ctx, in.Buyer)
		if err != nil {
			return err
		}
		if int(n) >= maxOpen {
			return ErrOfferLimit.WithMessage(fmt.Sprintf("you already have %d open offers", maxOpen))
		}
		dup, err := q.HasOpenMarketOfferOnName(ctx, generated.HasOpenMarketOfferOnNameParams{BuyerAccountID: in.Buyer, MaxID: in.MaxID})
		if err != nil {
			return err
		}
		if dup {
			return errOfferDuplicate
		}
		row, err := q.CreateMarketOffer(ctx, generated.CreateMarketOfferParams{
			MaxID: in.MaxID, BuyerAccountID: in.Buyer, FromAddress: in.FromAddress, Chain: in.Chain, AssetAddress: in.AssetAddress,
			AssetDecimals: int32(in.AssetDecimals), AmountBaseUnits: in.AmountBaseUnits, ExpiresAt: in.ExpiresAt,
		})
		if err != nil {
			return err
		}
		out = toOffer(row, "")
		buyer := in.Buyer
		return audit.Write(ctx, q, audit.Entry{
			Actor: &buyer, Action: "market.offer_created", ResourceType: "market_offer", ResourceID: row.ID.String(),
			Metadata: map[string]any{"max_id": in.MaxID.String(), "amount_base_units": in.AmountBaseUnits, "chain": in.Chain, "asset": in.AssetAddress},
		})
	})
	if database.IsUniqueViolation(err, "market_offers_one_per_buyer_idx") {
		return Offer{}, errOfferDuplicate
	}
	if err != nil {
		return Offer{}, mapErr(err, maxids.ErrMaxIDNotFound)
	}
	return out, nil
}

func (r *repository) GetOffer(ctx context.Context, id uuid.UUID) (Offer, error) {
	row, err := r.q.GetMarketOffer(ctx, id)
	if err != nil {
		return Offer{}, mapErr(err, ErrOfferNotFound)
	}
	return toOffer(row, ""), nil
}

func (r *repository) WithdrawOffer(ctx context.Context, id, buyer uuid.UUID) error {
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		o, err := q.GetMarketOfferForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if o.BuyerAccountID != buyer {
			return ErrOfferNotFound
		}
		if _, err := q.TransitionMarketOffer(ctx, generated.TransitionMarketOfferParams{
			ID: id, FromStatuses: openOfferStatuses, ToStatus: OfferWithdrawn,
		}); database.IsNoRows(err) {
			return errOfferNotLive
		} else if err != nil {
			return err
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &buyer, Action: "market.offer_withdrawn", ResourceType: "market_offer", ResourceID: id.String(),
			Metadata: map[string]any{"from_status": o.Status},
		})
	})
	return mapErr(err, ErrOfferNotFound)
}

func (r *repository) DeclineOffer(ctx context.Context, id, seller uuid.UUID, note string) error {
	head, err := r.q.GetMarketOffer(ctx, id)
	if err != nil {
		return mapErr(err, ErrOfferNotFound)
	}
	err = database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		m, err := q.LockMaxIDByID(ctx, head.MaxID)
		if err != nil {
			return err
		}
		if m.OwnerAccountID != seller {
			return ErrOfferNotFound
		}
		if _, err := q.DeclineMarketOffer(ctx, generated.DeclineMarketOfferParams{ID: id, Note: note}); database.IsNoRows(err) {
			return errOfferNotLive
		} else if err != nil {
			return err
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &seller, Action: "market.offer_declined", ResourceType: "market_offer", ResourceID: id.String(),
			Metadata: map[string]any{"max_id": head.MaxID.String()},
		})
	})
	return mapErr(err, ErrOfferNotFound)
}

func (r *repository) LiveListing(ctx context.Context, maxID uuid.UUID) (*Listing, error) {
	row, err := r.q.GetLiveMarketListingByMaxID(ctx, maxID)
	if database.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	l := toListing(row, "")
	return &l, nil
}

func (r *repository) AcceptOffer(ctx context.Context, offerID, seller uuid.UUID, in NewSale) (Sale, error) {
	var out Sale
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		// Lock order everywhere: buyer account, max_ids row, then offer /
		// listing / sale.
		if err := lockBuyerQuota(ctx, q, in.Buyer, false); err != nil {
			return err
		}
		if err := lockListable(ctx, q, in.MaxID, seller, ErrOfferNotFound); err != nil {
			return err
		}
		if err := lockBuyerCap(ctx, q, in.Buyer, false); err != nil {
			return err
		}
		o, err := q.GetMarketOfferForUpdate(ctx, offerID)
		if err != nil {
			return err
		}
		switch {
		case o.MaxID != in.MaxID, o.BuyerAccountID != in.Buyer, o.FromAddress != in.BuyerAddress,
			o.AmountBaseUnits != in.Price, o.Chain != in.Chain, o.AssetAddress != in.AssetAddress:
			return errOfferNotLive.WithMessage("the offer changed; reload it and try again")
		case o.Status == OfferUnfunded:
			return ErrOfferUnfunded
		case o.Status != OfferLive, !o.ExpiresAt.After(time.Now()):
			return errOfferNotLive
		}
		listing, err := q.GetLiveMarketListingByMaxIDForUpdate(ctx, in.MaxID)
		hasListing := err == nil
		if err != nil && !database.IsNoRows(err) {
			return err
		}
		if hasListing && listing.Status != ListingActive {
			return ErrNameLocked
		}
		if _, err := q.TransitionMarketOffer(ctx, generated.TransitionMarketOfferParams{
			ID: offerID, FromStatuses: []string{OfferLive}, ToStatus: OfferAccepted,
		}); err != nil {
			return err
		}
		if err := q.SupersedeMarketOffers(ctx, generated.SupersedeMarketOffersParams{MaxID: in.MaxID, KeepOfferID: &offerID}); err != nil {
			return err
		}
		row, err := q.CreateMarketSale(ctx, generated.CreateMarketSaleParams{
			MaxID: in.MaxID, OfferID: &offerID, SellerAccountID: seller, BuyerAccountID: in.Buyer,
			BuyerAddress: in.BuyerAddress, SellerPayoutAddress: in.SellerPayout, TreasuryAddress: in.Treasury,
			Chain: in.Chain, AssetAddress: in.AssetAddress, AssetDecimals: int32(in.AssetDecimals),
			PriceBaseUnits: in.Price, FeeBps: int32(in.FeeBPS), FeeBaseUnits: in.Fee, SellerBaseUnits: in.SellerAmount,
			QuotedNetworkFee: in.QuotedNetworkFee, IdempotencyKey: in.IdempotencyKey, Status: in.Status, LockExpiresAt: in.LockExpiresAt,
		})
		if err != nil {
			return err
		}
		if hasListing {
			if _, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
				ID: listing.ID, FromStatus: ListingActive, ToStatus: ListingLocked, Reason: "sale " + row.ID.String(),
			}); err != nil {
				return err
			}
		}
		out = toSale(row, "")
		if err := event(ctx, q, row.ID, "created", nil, row.Status, map[string]any{
			"buyer_address": in.BuyerAddress, "offer_id": offerID.String(),
		}); err != nil {
			return err
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &seller, Action: "market.offer_accepted", ResourceType: "market_offer", ResourceID: offerID.String(),
			Metadata: map[string]any{"max_id": in.MaxID.String(), "sale_id": row.ID.String(), "amount_base_units": in.Price},
		})
	})
	switch {
	case database.IsUniqueViolation(err, "market_sales_one_open_idx"):
		return Sale{}, ErrNameLocked
	case err != nil:
		return Sale{}, mapErr(err, ErrOfferNotFound)
	}
	return out, nil
}

func (r *repository) SaleByOffer(ctx context.Context, offerID uuid.UUID) (Sale, error) {
	row, err := r.q.GetMarketSaleByOfferID(ctx, &offerID)
	if err != nil {
		return Sale{}, mapErr(err, ErrSaleNotFound)
	}
	return toSale(row, ""), nil
}

func (r *repository) SetOfferFunded(ctx context.Context, id uuid.UUID, funded bool, checkedAt time.Time) error {
	to := OfferUnfunded
	if funded {
		to = OfferLive
	}
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		o, err := q.GetMarketOfferForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if o.Status != OfferLive && o.Status != OfferUnfunded {
			return nil // closed meanwhile
		}
		if _, err := q.SetMarketOfferFunding(ctx, generated.SetMarketOfferFundingParams{ID: id, ToStatus: to, CheckedAt: &checkedAt}); err != nil {
			return err
		}
		if o.Status == to {
			return nil
		}
		return audit.Write(ctx, q, audit.Entry{
			Action: "market.offer_funding_changed", ResourceType: "market_offer", ResourceID: id.String(),
			Metadata: map[string]any{"from_status": o.Status, "to_status": to},
		})
	})
	return mapErr(err, ErrOfferNotFound)
}

func (r *repository) DueFundingChecks(ctx context.Context, olderThan time.Time, limit int) ([]Offer, error) {
	rows, err := r.q.ClaimMarketOfferFundingChecks(ctx, generated.ClaimMarketOfferFundingChecksParams{OlderThan: olderThan, RowLimit: int32(limit)})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Offer, 0, len(rows))
	for _, row := range rows {
		out = append(out, toOffer(row, ""))
	}
	return out, nil
}

func (r *repository) ExpireOffers(ctx context.Context, limit int) (int, error) {
	n := 0
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		rows, err := q.ExpireMarketOffers(ctx, int32(limit))
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := audit.Write(ctx, q, audit.Entry{
				Action: "market.offer_expired", ResourceType: "market_offer", ResourceID: row.ID.String(),
				Metadata: map[string]any{"max_id": row.MaxID.String(), "buyer": row.BuyerAccountID.String()},
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

func (r *repository) LapseOffer(ctx context.Context, offerID uuid.UUID, strikesSince time.Time, rule SuspendRule) (LapseResult, error) {
	head, err := r.q.GetMarketOffer(ctx, offerID)
	if err != nil {
		return LapseResult{}, mapErr(err, ErrOfferNotFound)
	}
	var res LapseResult
	err = database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		// The per-buyer lock serialises concurrent lapses, so the strike count
		// and the suspension check below see each other's writes.
		if err := q.LockMarketOfferBuyer(ctx, head.BuyerAccountID.String()); err != nil {
			return err
		}
		o, err := q.GetMarketOfferForUpdate(ctx, offerID)
		if err != nil {
			return err
		}
		res.Offer = toOffer(o, "")
		switch o.Status {
		case OfferLapsed:
			return nil // already lapsed: no second strike
		case OfferAccepted:
		default:
			return errOfferNotLive.WithMessage("only an accepted offer can lapse; this one is " + o.Status)
		}
		// Only the buyer's own failure to pay earns a strike: the sale expired
		// with no transaction submitted.
		sale, err := q.GetMarketSaleByOfferID(ctx, &offerID)
		if database.IsNoRows(err) {
			return errNotLapsable.WithMessage("the offer has no sale")
		}
		if err != nil {
			return err
		}
		if !SaleLapsesOffer(sale.Status, sale.TxHash != nil) {
			return errNotLapsable.WithMessage("the offer's sale is " + sale.Status + "; only an unpaid expired sale lapses the offer")
		}
		row, err := q.TransitionMarketOffer(ctx, generated.TransitionMarketOfferParams{
			ID: offerID, FromStatuses: []string{OfferAccepted}, ToStatus: OfferLapsed,
		})
		if err != nil {
			return err
		}
		res.Offer = toOffer(row, "")
		if err := q.InsertMarketStrike(ctx, generated.InsertMarketStrikeParams{AccountID: o.BuyerAccountID, OfferID: &offerID}); err != nil {
			return err
		}
		n, err := q.CountMarketStrikesSince(ctx, generated.CountMarketStrikesSinceParams{AccountID: o.BuyerAccountID, CreatedAt: strikesSince})
		if err != nil {
			return err
		}
		res.Strikes = int(n)
		suspended, err := q.HasActiveMarketRestriction(ctx, generated.HasActiveMarketRestrictionParams{
			AccountID: o.BuyerAccountID, Kind: RestrictionOfferSuspension,
		})
		if err != nil {
			return err
		}
		if nr := rule(res.Strikes, suspended); nr != nil {
			rr, err := restrict(ctx, q, o.BuyerAccountID, *nr, nil)
			if err != nil {
				return err
			}
			res.Suspension = &rr
		}
		return audit.Write(ctx, q, audit.Entry{
			Action: "market.offer_lapsed", ResourceType: "market_offer", ResourceID: offerID.String(),
			Metadata: map[string]any{"buyer": o.BuyerAccountID.String(), "strikes_in_window": res.Strikes, "suspended": res.Suspension != nil},
		})
	})
	if err != nil {
		return LapseResult{}, mapErr(err, ErrOfferNotFound)
	}
	return res, nil
}

func toOffer(o generated.MarketOffer, name string) Offer {
	return Offer{
		ID: o.ID, MaxID: o.MaxID, BuyerAccountID: o.BuyerAccountID, Name: name, FromAddress: o.FromAddress,
		Chain: o.Chain, AssetAddress: o.AssetAddress, AssetDecimals: int(o.AssetDecimals), AmountBaseUnits: o.AmountBaseUnits,
		Status: o.Status, DeclineNote: o.DeclineNote, ExpiresAt: o.ExpiresAt, FundedCheckedAt: o.FundedCheckedAt,
		CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
	}
}

func toRestriction(r generated.MarketRestriction) Restriction {
	return Restriction{ID: r.ID, AccountID: r.AccountID, Kind: r.Kind, Until: r.Until, Reason: r.Reason, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, LiftedAt: r.LiftedAt}
}
