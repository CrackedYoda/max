package market

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
	"github.com/max/max-core/internal/shared"
)

var decimalRe = regexp.MustCompile(`^[0-9]{1,40}(\.[0-9]{1,40})?$`)

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 50
	inboxLimit         = 100
)

type repository struct {
	pool *pgxpool.Pool
	q    *generated.Queries
}

var _ Repository = (*repository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &repository{pool: pool, q: generated.New(pool)}
}

func nameFacts(ctx context.Context, q *generated.Queries, maxID uuid.UUID) (NameFacts, generated.GetMarketNameFactsRow, error) {
	row, err := q.GetMarketNameFacts(ctx, maxID)
	if err != nil {
		return NameFacts{}, row, err
	}
	return NameFacts{
		Status: row.Status, OwnerStatus: row.OwnerStatus, HasLiveReservedClaim: row.HasLiveReservedClaim,
		RenamePending: row.RenamePending, ListableAt: row.ListableAt, RenameAvailableAt: row.RenameAvailableAt,
		OwnerBanned: row.OwnerBanned,
	}, row, nil
}

func (r *repository) NameFacts(ctx context.Context, maxID uuid.UUID) (NameFacts, uuid.UUID, error) {
	f, row, err := nameFacts(ctx, r.q, maxID)
	if err != nil {
		return NameFacts{}, uuid.Nil, mapErr(err, maxids.ErrMaxIDNotFound)
	}
	return f, row.OwnerAccountID, nil
}

func (r *repository) PartyFacts(ctx context.Context, buyer, seller uuid.UUID, payer string) (PartyFacts, error) {
	links, err := r.q.GetMarketWalletLinks(ctx, generated.GetMarketWalletLinksParams{Buyer: buyer, Seller: seller, PayerAddress: payer})
	if err != nil {
		return PartyFacts{}, shared.ErrInternal.WithError(err)
	}
	p := PartyFacts{BuyerAccount: buyer, SellerAccount: seller, SharedWallet: links.SharedWallet, PayerWalletEverSellers: links.PayerEverSellers}
	if p.BuyerAdmin, err = r.administrator(ctx, buyer); err != nil {
		return PartyFacts{}, err
	}
	if p.SellerAdmin, err = r.administrator(ctx, seller); err != nil {
		return PartyFacts{}, err
	}
	return p, nil
}

func (r *repository) administrator(ctx context.Context, account uuid.UUID) (*uuid.UUID, error) {
	id, err := r.q.GetAgentAdministratorID(ctx, account)
	if database.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	return &id, nil
}

func (r *repository) ActiveWalletID(ctx context.Context, account uuid.UUID, ns chains.Namespace, address string) (uuid.UUID, error) {
	w, err := r.q.GetActiveWalletByAddress(ctx, generated.GetActiveWalletByAddressParams{ChainNamespace: string(ns), Address: address})
	if database.IsNoRows(err) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, shared.ErrInternal.WithError(err)
	}
	if w.AccountID != account {
		return uuid.Nil, nil
	}
	return w.ID, nil
}

func (r *repository) Restricted(ctx context.Context, account uuid.UUID, kind string) (bool, error) {
	ok, err := r.q.HasActiveMarketRestriction(ctx, generated.HasActiveMarketRestrictionParams{AccountID: account, Kind: kind})
	if err != nil {
		return false, shared.ErrInternal.WithError(err)
	}
	return ok, nil
}

// lockListable takes the max_ids row lock and re-checks listability under it,
// so a rename, transfer or ban racing the caller cannot slip through.
func lockListable(ctx context.Context, q *generated.Queries, maxID, owner uuid.UUID, ownerErr error) error {
	m, err := q.LockMaxIDByID(ctx, maxID)
	if err != nil {
		return err
	}
	if m.OwnerAccountID != owner {
		return ownerErr
	}
	f, _, err := nameFacts(ctx, q, maxID)
	if err != nil {
		return err
	}
	if ok, reason := Listable(f, time.Now()); !ok {
		return NotListable(reason)
	}
	return maxids.EnsureNoOpenSale(ctx, q, maxID)
}

func (r *repository) CreateListing(ctx context.Context, in NewListing) (Listing, error) {
	var out Listing
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := lockListable(ctx, q, in.MaxID, in.Seller, maxids.ErrMaxIDNotFound); err != nil {
			return err
		}
		if err := maxids.EnsureNotListed(ctx, q, in.MaxID); err != nil {
			return err
		}
		row, err := q.CreateMarketListing(ctx, generated.CreateMarketListingParams{
			MaxID: in.MaxID, SellerAccountID: in.Seller, Chain: in.Chain, AssetAddress: in.AssetAddress,
			AssetDecimals: int32(in.AssetDecimals), PriceBaseUnits: in.PriceBaseUnits, PayoutAddress: in.PayoutAddress, ExpiresAt: in.ExpiresAt,
		})
		if err != nil {
			return err
		}
		out = toListing(row, "")
		seller := in.Seller
		return audit.Write(ctx, q, audit.Entry{
			Actor: &seller, Action: "market.listing_created", ResourceType: "market_listing", ResourceID: row.ID.String(),
			Metadata: map[string]any{"max_id": in.MaxID.String(), "price_base_units": in.PriceBaseUnits, "chain": in.Chain, "asset": in.AssetAddress},
		})
	})
	if database.IsUniqueViolation(err, "market_listings_one_live_idx") {
		return Listing{}, maxids.ErrNameListed
	}
	if err != nil {
		return Listing{}, mapErr(err, maxids.ErrMaxIDNotFound)
	}
	return out, nil
}

func (r *repository) GetListing(ctx context.Context, id uuid.UUID) (Listing, error) {
	row, err := r.q.GetMarketListing(ctx, id)
	if err != nil {
		return Listing{}, mapErr(err, ErrListingNotFound)
	}
	return toListing(row, ""), nil
}

// lockOwnedListing locks a listing and checks it is the seller's and active.
func lockOwnedListing(ctx context.Context, q *generated.Queries, id, seller uuid.UUID) (generated.MarketListing, error) {
	l, err := q.GetMarketListingForUpdate(ctx, id)
	if err != nil {
		return l, err
	}
	switch {
	case l.SellerAccountID != seller:
		return l, ErrListingNotFound
	case l.Status == ListingLocked:
		return l, ErrNameLocked
	case l.Status != ListingActive:
		return l, ErrListingNotActive
	}
	return l, nil
}

func (r *repository) UpdateListingPrice(ctx context.Context, id, seller uuid.UUID, price string) (Listing, error) {
	var out Listing
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		old, err := lockOwnedListing(ctx, q, id, seller)
		if err != nil {
			return err
		}
		row, err := q.UpdateMarketListingPrice(ctx, generated.UpdateMarketListingPriceParams{ID: id, PriceBaseUnits: price})
		if err != nil {
			return err
		}
		out = toListing(row, "")
		return audit.Write(ctx, q, audit.Entry{
			Actor: &seller, Action: "market.listing_price_updated", ResourceType: "market_listing", ResourceID: id.String(),
			Metadata: map[string]any{"old_price_base_units": old.PriceBaseUnits, "price_base_units": price},
		})
	})
	if err != nil {
		return Listing{}, mapErr(err, ErrListingNotFound)
	}
	return out, nil
}

func (r *repository) CancelListing(ctx context.Context, id, seller uuid.UUID) error {
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := lockOwnedListing(ctx, q, id, seller); err != nil {
			return err
		}
		if _, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
			ID: id, FromStatus: ListingActive, ToStatus: ListingCancelled, Reason: "cancelled by seller",
		}); err != nil {
			return err
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &seller, Action: "market.listing_cancelled", ResourceType: "market_listing", ResourceID: id.String(),
		})
	})
	return mapErr(err, ErrListingNotFound)
}

func (r *repository) OpenSale(ctx context.Context, in NewSale) (Sale, error) {
	var out Sale
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := lockListable(ctx, q, in.MaxID, in.Seller, ErrListingNotActive); err != nil {
			return err
		}
		if in.ListingID != nil {
			l, err := q.GetMarketListingForUpdate(ctx, *in.ListingID)
			if err != nil {
				return err
			}
			switch {
			case l.Status == ListingLocked:
				return ErrNameLocked
			case l.Status != ListingActive, l.ExpiresAt != nil && !l.ExpiresAt.After(time.Now()):
				return ErrListingNotActive
			case l.MaxID != in.MaxID, l.SellerAccountID != in.Seller, l.PriceBaseUnits != in.Price,
				l.PayoutAddress != in.SellerPayout, l.Chain != in.Chain, l.AssetAddress != in.AssetAddress:
				return ErrListingNotActive.WithMessage("the listing changed; reload it and try again")
			}
		}
		row, err := q.CreateMarketSale(ctx, generated.CreateMarketSaleParams{
			MaxID: in.MaxID, ListingID: in.ListingID, OfferID: in.OfferID, SellerAccountID: in.Seller, BuyerAccountID: in.Buyer,
			BuyerAddress: in.BuyerAddress, SellerPayoutAddress: in.SellerPayout, TreasuryAddress: in.Treasury,
			Chain: in.Chain, AssetAddress: in.AssetAddress, AssetDecimals: int32(in.AssetDecimals),
			PriceBaseUnits: in.Price, FeeBps: int32(in.FeeBPS), FeeBaseUnits: in.Fee, SellerBaseUnits: in.SellerAmount,
			QuotedNetworkFee: in.QuotedNetworkFee, IdempotencyKey: in.IdempotencyKey, Status: in.Status, LockExpiresAt: in.LockExpiresAt,
		})
		if err != nil {
			return err
		}
		if in.ListingID != nil {
			if _, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
				ID: *in.ListingID, FromStatus: ListingActive, ToStatus: ListingLocked, Reason: "sale " + row.ID.String(),
			}); err != nil {
				return err
			}
		}
		out = toSale(row, "")
		return event(ctx, q, row.ID, "created", nil, row.Status, map[string]any{"buyer_address": in.BuyerAddress})
	})
	switch {
	case database.IsUniqueViolation(err, "market_sales_idempotency_idx"):
		prior, perr := r.SaleByIdempotencyKey(ctx, in.Buyer, in.IdempotencyKey)
		if perr != nil {
			return Sale{}, perr
		}
		if prior == nil || !sameSource(*prior, in) {
			return Sale{}, payments.ErrIdempotencyConflict.WithMessage("idempotency key was already used for a different listing")
		}
		return *prior, nil
	case database.IsUniqueViolation(err, "market_sales_one_open_idx"):
		return Sale{}, ErrNameLocked
	case err != nil:
		return Sale{}, mapErr(err, ErrListingNotActive)
	}
	return out, nil
}

func sameSource(s Sale, in NewSale) bool {
	eq := func(a, b *uuid.UUID) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
	return eq(s.ListingID, in.ListingID) && eq(s.OfferID, in.OfferID)
}

func (r *repository) SaleByIdempotencyKey(ctx context.Context, buyer uuid.UUID, key string) (*Sale, error) {
	row, err := r.q.GetMarketSaleByIdempotencyKey(ctx, generated.GetMarketSaleByIdempotencyKeyParams{BuyerAccountID: buyer, IdempotencyKey: key})
	if database.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	s := toSale(row, "")
	return &s, nil
}

func (r *repository) AttachTx(ctx context.Context, saleID, buyer uuid.UUID, txHash string) (Sale, error) {
	var out Sale
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.AttachMarketSaleTx(ctx, generated.AttachMarketSaleTxParams{ID: saleID, Buyer: buyer, TxHash: &txHash})
		if database.IsNoRows(err) {
			// Lost a race with expiry or another submit: report the current state.
			cur, gerr := q.GetMarketSale(ctx, saleID)
			if gerr != nil {
				return gerr
			}
			if cur.TxHash != nil && *cur.TxHash == txHash {
				out = toSale(cur, "")
				return nil
			}
			if cur.TxHash != nil {
				return ErrTxMismatch.WithMessage("a different transaction was already submitted for this sale")
			}
			return ErrSaleExpired.WithMessage("this sale is " + cur.Status + " and no longer accepts a transaction")
		}
		if err != nil {
			return err
		}
		out = toSale(row, "")
		from := SaleLocked
		return event(ctx, q, saleID, "submitted", &from, row.Status, map[string]any{"tx_hash": txHash})
	})
	if database.IsUniqueViolation(err, "market_sales_tx_hash_idx") {
		return Sale{}, ErrTxMismatch.WithMessage("this transaction is already attached to another sale")
	}
	if err != nil {
		return Sale{}, mapErr(err, ErrSaleNotFound)
	}
	return out, nil
}

func (r *repository) ProgressSale(ctx context.Context, saleID uuid.UUID, p Progress) (Sale, error) {
	var out Sale
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.ProgressMarketSale(ctx, generated.ProgressMarketSaleParams{
			ID: saleID, FromStatus: p.From, ToStatus: p.To, Reason: p.Reason, Confirmations: p.Confirmations, NextCheckAt: p.NextCheckAt,
		})
		if database.IsNoRows(err) {
			cur, gerr := q.GetMarketSale(ctx, saleID)
			out = toSale(cur, "")
			return gerr // moved concurrently; report where it is now
		}
		if err != nil {
			return err
		}
		out = toSale(row, "")
		if p.From == p.To || p.Event == "" {
			return nil
		}
		from := p.From
		return event(ctx, q, saleID, p.Event, &from, p.To, p.Detail)
	})
	if err != nil {
		return Sale{}, mapErr(err, ErrSaleNotFound)
	}
	return out, nil
}

func (r *repository) CompleteSale(ctx context.Context, saleID uuid.UUID, hold time.Duration, confirmations int64) (Sale, error) {
	head, err := r.q.GetMarketSale(ctx, saleID)
	if err != nil {
		return Sale{}, mapErr(err, ErrSaleNotFound)
	}
	var out Sale
	err = database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		// Lock order everywhere: max_ids row, then listing / sale.
		if _, err := q.LockMaxIDByID(ctx, head.MaxID); err != nil {
			return err
		}
		sale, err := q.GetMarketSaleForUpdate(ctx, saleID)
		if err != nil {
			return err
		}
		out = toSale(sale, "")
		switch sale.Status {
		case SaleCompleted:
			return nil // idempotent
		case SaleSubmitted, SaleConfirming:
		default:
			return shared.ErrConflict.WithMessage("sale is " + sale.Status + " and cannot complete")
		}
		ns := ""
		if c, ok := chains.Lookup(sale.Chain); ok {
			ns = string(c.Namespace)
		}
		dest := uuid.Nil
		if w, err := q.GetActiveWalletByAddress(ctx, generated.GetActiveWalletByAddressParams{ChainNamespace: ns, Address: sale.BuyerAddress}); err == nil {
			if w.AccountID == sale.BuyerAccountID {
				dest = w.ID
			}
		} else if !database.IsNoRows(err) {
			return err
		}
		saleRef := sale.ID
		terr := maxids.TransferOwnership(ctx, q, maxids.TransferParams{
			MaxID: sale.MaxID, From: sale.SellerAccountID, To: sale.BuyerAccountID, SaleID: &saleRef,
			DestinationWalletID: dest, HoldPeriod: hold, Reason: "market sale",
		})
		if errors.Is(terr, maxids.ErrTransferOwnerMismatch) || errors.Is(terr, maxids.ErrRenamePending) {
			return r.toManualReview(ctx, q, sale, terr, &out)
		}
		if terr != nil {
			return terr
		}
		done, err := q.CompleteMarketSale(ctx, generated.CompleteMarketSaleParams{ID: saleID, Confirmations: confirmations})
		if err != nil {
			return err
		}
		if sale.ListingID != nil {
			if _, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
				ID: *sale.ListingID, FromStatus: ListingLocked, ToStatus: ListingSold, Reason: "sale " + sale.ID.String(),
			}); err != nil {
				return err
			}
		} else if l, err := q.GetLiveMarketListingByMaxIDForUpdate(ctx, sale.MaxID); err == nil {
			// An accepted offer locked the seller's listing; the name sold,
			// so the listing is sold too (spec §6 Complete 2).
			if _, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
				ID: l.ID, FromStatus: ListingLocked, ToStatus: ListingSold, Reason: "sold through an accepted offer, sale " + sale.ID.String(),
			}); err != nil {
				return err
			}
		} else if !database.IsNoRows(err) {
			return err
		}
		if sale.OfferID != nil {
			if err := q.CompleteMarketOffer(ctx, *sale.OfferID); err != nil {
				return err
			}
		}
		if err := q.SupersedeMarketOffers(ctx, generated.SupersedeMarketOffersParams{MaxID: sale.MaxID, KeepOfferID: sale.OfferID}); err != nil {
			return err
		}
		detail := map[string]any{"confirmations": confirmations}
		if dest == uuid.Nil {
			detail["destination"] = "none: the paying wallet is no longer linked to the buyer"
		} else {
			detail["destination_wallet_id"] = dest.String()
		}
		from := sale.Status
		if err := event(ctx, q, sale.ID, "completed", &from, SaleCompleted, detail); err != nil {
			return err
		}
		out = toSale(done, "")
		return audit.Write(ctx, q, audit.Entry{
			Action: "market.sale_completed", ResourceType: "market_sale", ResourceID: sale.ID.String(),
			Metadata: map[string]any{
				"max_id": sale.MaxID.String(), "seller": sale.SellerAccountID.String(), "buyer": sale.BuyerAccountID.String(),
				"chain": sale.Chain, "asset": sale.AssetAddress, "price_base_units": sale.PriceBaseUnits,
				"fee_base_units": sale.FeeBaseUnits, "tx_hash": deref(sale.TxHash), "destination_wallet_id": nilUUID(dest),
			},
		})
	})
	if err != nil {
		return Sale{}, mapErr(err, ErrSaleNotFound)
	}
	return out, nil
}

// toManualReview parks a paid sale whose ownership move was refused. The
// transfer wrote nothing (it refuses before its first write), so this
// transaction is still clean.
func (r *repository) toManualReview(ctx context.Context, q *generated.Queries, sale generated.MarketSale, cause error, out *Sale) error {
	reason := "ownership transfer refused: " + cause.Error()
	row, err := q.TransitionMarketSale(ctx, generated.TransitionMarketSaleParams{
		ID: sale.ID, FromStatus: sale.Status, ToStatus: SaleManualReview, Reason: reason,
	})
	if err != nil {
		return err
	}
	from := sale.Status
	if err := event(ctx, q, sale.ID, "transfer_refused", &from, SaleManualReview, map[string]any{"reason": reason}); err != nil {
		return err
	}
	*out = toSale(row, "")
	return audit.Write(ctx, q, audit.Entry{
		Action: "market.sale_manual_review", ResourceType: "market_sale", ResourceID: sale.ID.String(),
		Metadata: map[string]any{"max_id": sale.MaxID.String(), "reason": reason, "tx_hash": deref(sale.TxHash)},
	})
}

func (r *repository) ExpireSale(ctx context.Context, saleID uuid.UUID, reason string) (Sale, error) {
	head, err := r.q.GetMarketSale(ctx, saleID)
	if err != nil {
		return Sale{}, mapErr(err, ErrSaleNotFound)
	}
	var out Sale
	err = database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockMaxIDByID(ctx, head.MaxID); err != nil {
			return err
		}
		sale, err := q.GetMarketSaleForUpdate(ctx, saleID)
		if err != nil {
			return err
		}
		out = toSale(sale, "")
		switch sale.Status {
		case SaleLocked, SaleSubmitted, SaleConfirming:
		default:
			return nil // already closed
		}
		row, err := q.TransitionMarketSale(ctx, generated.TransitionMarketSaleParams{
			ID: saleID, FromStatus: sale.Status, ToStatus: SaleExpired, Reason: reason,
		})
		if err != nil {
			return err
		}
		// The listing is locked only by this (now closed) sale: put it back.
		if l, err := q.GetLiveMarketListingByMaxIDForUpdate(ctx, sale.MaxID); err == nil && l.Status == ListingLocked {
			if _, err := q.TransitionMarketListing(ctx, generated.TransitionMarketListingParams{
				ID: l.ID, FromStatus: ListingLocked, ToStatus: ListingActive, Reason: "",
			}); err != nil {
				return err
			}
		} else if err != nil && !database.IsNoRows(err) {
			return err
		}
		out = toSale(row, "")
		from := sale.Status
		return event(ctx, q, saleID, "expired", &from, SaleExpired, map[string]any{"reason": reason})
	})
	if err != nil {
		return Sale{}, mapErr(err, ErrSaleNotFound)
	}
	return out, nil
}

func (r *repository) GetSale(ctx context.Context, id uuid.UUID) (Sale, error) {
	row, err := r.q.GetMarketSale(ctx, id)
	if err != nil {
		return Sale{}, mapErr(err, ErrSaleNotFound)
	}
	return toSale(row, ""), nil
}

func (r *repository) SearchListings(ctx context.Context, lq ListingQuery) ([]ListingCard, string, error) {
	limit := lq.Limit
	if limit <= 0 || limit > maxSearchLimit {
		limit = defaultSearchLimit
	}
	offset, err := decodeCursor(lq.Cursor)
	if err != nil {
		return nil, "", err
	}
	sort := lq.Sort
	switch sort {
	case "", "newest":
		sort = "newest"
	case "price_asc", "price_desc":
	default:
		return nil, "", shared.ErrValidation.WithMessage("sort must be newest, price_asc or price_desc")
	}
	var minP, maxP *string
	for _, pair := range []struct {
		raw string
		dst **string
	}{{lq.MinPrice, &minP}, {lq.MaxPrice, &maxP}} {
		if pair.raw == "" {
			continue
		}
		if !decimalRe.MatchString(pair.raw) {
			return nil, "", shared.ErrValidation.WithMessage("minPrice and maxPrice must be decimal amounts")
		}
		v := pair.raw
		*pair.dst = &v
	}
	q := strings.ToLower(strings.TrimSpace(lq.Q))
	q = strings.NewReplacer(`\`, "", "%", "", "_", "").Replace(q)
	rows, err := r.q.SearchMarketListings(ctx, generated.SearchMarketListingsParams{
		Q: q, MinPrice: minP, MaxPrice: maxP, Sort: sort, RowLimit: int32(limit + 1), RowOffset: int32(offset),
	})
	if err != nil {
		return nil, "", shared.ErrInternal.WithError(err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(offset + limit)
	}
	out := make([]ListingCard, 0, len(rows))
	for _, row := range rows {
		l := row.MarketListing
		out = append(out, ListingCard{
			ListingID: l.ID, MaxID: l.MaxID, Name: row.CanonicalName, Chain: l.Chain, AssetAddress: l.AssetAddress,
			AssetDecimals: int(l.AssetDecimals), PriceBaseUnits: l.PriceBaseUnits, ExpiresAt: l.ExpiresAt, CreatedAt: l.CreatedAt,
		})
	}
	return out, next, nil
}

// Cursors are opaque offsets; good enough for v1's listing volume.
func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset)))
}

func decodeCursor(c string) (int, error) {
	if c == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err == nil && strings.HasPrefix(string(b), "o:") {
		if n, err := strconv.Atoi(string(b[2:])); err == nil && n >= 0 && n <= 100000 {
			return n, nil
		}
	}
	return 0, shared.ErrValidation.WithMessage("cursor is invalid")
}

func (r *repository) NameView(ctx context.Context, name string) (NameView, error) {
	id, err := r.q.GetMaxIDIDByCanonicalName(ctx, name)
	if err != nil {
		return NameView{}, mapErr(err, maxids.ErrMaxIDNotFound)
	}
	f, row, err := nameFacts(ctx, r.q, id)
	if err != nil {
		return NameView{}, mapErr(err, maxids.ErrMaxIDNotFound)
	}
	v := NameView{MaxID: id, Name: row.CanonicalName, Facts: f}
	if l, err := r.q.GetLiveMarketListingByMaxID(ctx, id); err == nil {
		lv := toListing(l, row.CanonicalName)
		v.Listing = &lv
	} else if !database.IsNoRows(err) {
		return NameView{}, shared.ErrInternal.WithError(err)
	}
	if v.LiveOfferCount, err = r.q.CountLiveMarketOffers(ctx, id); err != nil {
		return NameView{}, shared.ErrInternal.WithError(err)
	}
	if s, err := r.q.GetLastCompletedMarketSale(ctx, id); err == nil && s.CompletedAt != nil {
		v.LastSale = &LastSale{Chain: s.Chain, AssetAddress: s.AssetAddress, AssetDecimals: int(s.AssetDecimals),
			PriceBaseUnits: s.PriceBaseUnits, CompletedAt: *s.CompletedAt}
	} else if err != nil && !database.IsNoRows(err) {
		return NameView{}, shared.ErrInternal.WithError(err)
	}
	return v, nil
}

func (r *repository) Inbox(ctx context.Context, account uuid.UUID) (Inbox, error) {
	ls, err := r.q.ListMarketListingsBySellerWithName(ctx, generated.ListMarketListingsBySellerWithNameParams{SellerAccountID: account, Limit: inboxLimit})
	if err != nil {
		return Inbox{}, shared.ErrInternal.WithError(err)
	}
	ss, err := r.q.ListMarketSalesByPartyWithName(ctx, generated.ListMarketSalesByPartyWithNameParams{SellerAccountID: account, Limit: inboxLimit})
	if err != nil {
		return Inbox{}, shared.ErrInternal.WithError(err)
	}
	recv, err := r.q.ListLiveMarketOffersReceived(ctx, generated.ListLiveMarketOffersReceivedParams{OwnerAccountID: account, Limit: inboxLimit})
	if err != nil {
		return Inbox{}, shared.ErrInternal.WithError(err)
	}
	made, err := r.q.ListMarketOffersByBuyerWithName(ctx, generated.ListMarketOffersByBuyerWithNameParams{BuyerAccountID: account, Limit: inboxLimit})
	if err != nil {
		return Inbox{}, shared.ErrInternal.WithError(err)
	}
	out := Inbox{Listings: make([]Listing, 0, len(ls)), Sales: make([]Sale, 0, len(ss)),
		OffersReceived: make([]Offer, 0, len(recv)), OffersMade: make([]Offer, 0, len(made))}
	for _, o := range recv {
		out.OffersReceived = append(out.OffersReceived, toOffer(o.MarketOffer, o.CanonicalName))
	}
	for _, o := range made {
		out.OffersMade = append(out.OffersMade, toOffer(o.MarketOffer, o.CanonicalName))
	}
	for _, l := range ls {
		out.Listings = append(out.Listings, toListing(l.MarketListing, l.CanonicalName))
	}
	for _, s := range ss {
		out.Sales = append(out.Sales, toSale(s.MarketSale, s.CanonicalName))
	}
	return out, nil
}

func event(ctx context.Context, q *generated.Queries, saleID uuid.UUID, kind string, from *string, to string, detail map[string]any) error {
	b := []byte("{}")
	if len(detail) > 0 {
		var err error
		if b, err = json.Marshal(detail); err != nil {
			return err
		}
	}
	return q.InsertMarketSaleEvent(ctx, generated.InsertMarketSaleEventParams{SaleID: saleID, EventType: kind, FromStatus: from, ToStatus: to, Detail: b})
}

func toListing(l generated.MarketListing, name string) Listing {
	return Listing{
		ID: l.ID, MaxID: l.MaxID, SellerAccountID: l.SellerAccountID, Name: name, Chain: l.Chain, AssetAddress: l.AssetAddress,
		AssetDecimals: int(l.AssetDecimals), PriceBaseUnits: l.PriceBaseUnits, PayoutAddress: l.PayoutAddress,
		Status: l.Status, StatusReason: l.StatusReason, ExpiresAt: l.ExpiresAt, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

func toSale(s generated.MarketSale, name string) Sale {
	return Sale{
		ID: s.ID, MaxID: s.MaxID, Name: name, ListingID: s.ListingID, OfferID: s.OfferID,
		SellerAccountID: s.SellerAccountID, BuyerAccountID: s.BuyerAccountID, BuyerAddress: s.BuyerAddress,
		SellerPayoutAddress: s.SellerPayoutAddress, TreasuryAddress: s.TreasuryAddress, Chain: s.Chain,
		AssetAddress: s.AssetAddress, AssetDecimals: int(s.AssetDecimals), PriceBaseUnits: s.PriceBaseUnits,
		FeeBPS: int(s.FeeBps), FeeBaseUnits: s.FeeBaseUnits, SellerBaseUnits: s.SellerBaseUnits,
		QuotedNetworkFee: s.QuotedNetworkFee, IdempotencyKey: s.IdempotencyKey, Status: s.Status, StatusReason: s.StatusReason,
		LockExpiresAt: s.LockExpiresAt, TxHash: s.TxHash, Confirmations: s.Confirmations, SubmittedAt: s.SubmittedAt,
		CompletedAt: s.CompletedAt, NextCheckAt: s.NextCheckAt, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nilUUID(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// mapErr passes AppErrors through, turns "no rows" into notFound and hides
// everything else behind INTERNAL_ERROR.
func mapErr(err, notFound error) error {
	if err == nil {
		return nil
	}
	var app *shared.AppError
	if errors.As(err, &app) {
		return err
	}
	if database.IsNoRows(err) {
		return notFound
	}
	return shared.ErrInternal.WithError(err)
}
