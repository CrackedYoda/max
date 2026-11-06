package market

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

// Admin views and actions (spec §9 Admin). The routes are session-only and
// limited to ADMIN_ACCOUNT_IDS, so views show full data. Every write takes
// the acting administrator and writes audit_logs, plus market_sale_events
// when a sale is involved.

const (
	adminDefaultLimit = 50
	adminMaxLimit     = 100
	maxReasonLen      = 500
)

var (
	ErrRestrictionNotFound = shared.ErrNotFound.WithMessage("restriction not found")
	errReasonRequired      = shared.ErrValidation.WithMessage("reason is required")
	errReasonTooLong       = shared.ErrValidation.WithMessage("reason must be at most 500 characters")
	errNotPendingReview    = shared.ErrConflict.WithMessage("only a sale awaiting review can be approved or denied")
	errNotCancellable      = shared.ErrConflict.WithMessage("only a locked sale with no transaction, or a sale in manual review, can be cancelled")
	errWorkerOwnsSale      = shared.ErrConflict.WithMessage("a transaction was submitted for this sale; the worker settles it")
	errNotReversible       = shared.ErrConflict.WithMessage("only a completed sale can be reversed")
	errBuyerNotOwner       = maxids.ErrTransferOwnerMismatch.WithMessage("buyer no longer owns the name")
	errDelistLocked        = ErrNameLocked.WithMessage("the listing has an open sale; cancel the sale first")
)

var (
	listingStatuses = []string{ListingActive, ListingLocked, ListingSold, ListingCancelled, ListingAdminDelisted}
	offerStatuses   = []string{OfferLive, OfferUnfunded, OfferAccepted, OfferDeclined, OfferSuperseded, OfferExpired,
		OfferWithdrawn, OfferLapsed, OfferCompleted}
	saleStatuses = []string{SalePendingReview, SaleLocked, SaleSubmitted, SaleConfirming, SaleCompleted, SaleExpired,
		SaleCancelled, SaleManualReview, SaleReversed}
)

// AdminFilter narrows an admin view. Empty fields match everything; Name is a
// MAX ID name; AccountID is the seller (listings), the buyer (offers) or
// either party (sales).
type AdminFilter struct {
	Status    string
	AccountID *uuid.UUID
	Name      string
	Limit     int
}

// adminDenyNote is the decline note an offer gets when an admin denies its sale.
func adminDenyNote(reason string) string { return "denied by admin: " + reason }

func (f AdminFilter) normalize(statuses []string) (AdminFilter, error) {
	f.Status = strings.TrimSpace(f.Status)
	if f.Status != "" && !slices.Contains(statuses, f.Status) {
		return f, shared.ErrValidation.WithMessage("status must be one of " + strings.Join(statuses, ", "))
	}
	if f.Name = strings.TrimSpace(f.Name); f.Name != "" {
		name, err := maxids.Normalize(f.Name)
		if err != nil {
			return f, shared.ErrValidation.WithMessage("name is not a valid MAX ID")
		}
		f.Name = name
	}
	switch {
	case f.Limit < 0:
		return f, shared.ErrValidation.WithMessage("limit must be between 1 and 100")
	case f.Limit == 0:
		f.Limit = adminDefaultLimit
	case f.Limit > adminMaxLimit:
		f.Limit = adminMaxLimit
	}
	return f, nil
}

// reason trims an admin reason; required reasons must not be empty.
func reason(raw string, required bool) (string, error) {
	r := strings.TrimSpace(raw)
	if required && r == "" {
		return "", errReasonRequired
	}
	if len(r) > maxReasonLen {
		return "", errReasonTooLong
	}
	return r, nil
}

func (s *Service) AdminListings(ctx context.Context, f AdminFilter) ([]Listing, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	f, err := f.normalize(listingStatuses)
	if err != nil {
		return nil, err
	}
	return s.repo.AdminListings(ctx, f)
}

func (s *Service) AdminOffers(ctx context.Context, f AdminFilter) ([]Offer, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	f, err := f.normalize(offerStatuses)
	if err != nil {
		return nil, err
	}
	return s.repo.AdminOffers(ctx, f)
}

func (s *Service) AdminSales(ctx context.Context, f AdminFilter) ([]Sale, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	f, err := f.normalize(saleStatuses)
	if err != nil {
		return nil, err
	}
	return s.repo.AdminSales(ctx, f)
}

// AdminDelist takes an active listing down; the seller sees the reason on
// their own listing. A locked listing has an open sale, which the admin must
// cancel first.
func (s *Service) AdminDelist(ctx context.Context, actor, listingID uuid.UUID, rawReason string) (Listing, error) {
	if err := s.enabled(); err != nil {
		return Listing{}, err
	}
	r, err := reason(rawReason, true)
	if err != nil {
		return Listing{}, err
	}
	return s.repo.AdminDelist(ctx, actor, listingID, r)
}

// AdminApprove moves a pending_review sale to locked and starts its lock now:
// MARKET_BUY_LOCK for a buy-now sale, MARKET_OFFER_PAY_WINDOW for an offer
// sale. The buyer then gets instructions from GET /v1/market/sales/:id (or
// POST /v1/market/offers/:id/pay).
func (s *Service) AdminApprove(ctx context.Context, actor, saleID uuid.UUID, rawReason string) (Sale, error) {
	if err := s.enabled(); err != nil {
		return Sale{}, err
	}
	r, err := reason(rawReason, false)
	if err != nil {
		return Sale{}, err
	}
	sale, err := s.repo.GetSale(ctx, saleID)
	if err != nil {
		return Sale{}, err
	}
	if sale.Status != SalePendingReview {
		return Sale{}, errNotPendingReview
	}
	window := s.cfg.BuyLock
	if sale.OfferID != nil {
		window = s.cfg.OfferPayWindow
	}
	return s.repo.AdminApproveSale(ctx, actor, saleID, s.now().Add(window), r)
}

// AdminDeny cancels a pending_review sale and releases the name: a buy-now
// listing returns to active; an offer is declined ("denied by admin:
// <reason>", no strike) and the seller's listing, if any, returns to active.
// Offers the acceptance superseded stay superseded.
func (s *Service) AdminDeny(ctx context.Context, actor, saleID uuid.UUID, rawReason string) (Sale, error) {
	if err := s.enabled(); err != nil {
		return Sale{}, err
	}
	r, err := reason(rawReason, true)
	if err != nil {
		return Sale{}, err
	}
	sale, err := s.repo.GetSale(ctx, saleID)
	if err != nil {
		return Sale{}, err
	}
	if sale.Status != SalePendingReview {
		return Sale{}, errNotPendingReview
	}
	return s.repo.AdminDenySale(ctx, actor, saleID, r)
}

// AdminCancel cancels a locked sale with no transaction, or a sale in manual
// review: the listing returns to active and an accepted offer expires (no
// strike). Any refund is handled off-platform. Submitted and confirming
// sales belong to the worker and are refused.
func (s *Service) AdminCancel(ctx context.Context, actor, saleID uuid.UUID, rawReason string) (Sale, error) {
	if err := s.enabled(); err != nil {
		return Sale{}, err
	}
	r, err := reason(rawReason, true)
	if err != nil {
		return Sale{}, err
	}
	sale, err := s.repo.GetSale(ctx, saleID)
	if err != nil {
		return Sale{}, err
	}
	if err := cancellable(sale); err != nil {
		return Sale{}, err
	}
	return s.repo.AdminCancelSale(ctx, actor, saleID, r)
}

// cancellable reports whether an admin may cancel the sale as it stands.
func cancellable(sale Sale) error {
	switch {
	case sale.Status == SaleManualReview:
		return nil
	case sale.Status == SaleLocked && sale.TxHash == nil:
		return nil
	case sale.Status == SaleLocked, sale.Status == SaleSubmitted, sale.Status == SaleConfirming:
		return errWorkerOwnsSale
	}
	return errNotCancellable
}

// AdminReverse moves a completed sale's name back to the seller while the
// buyer still owns it (maxids.TransferOwnership, no destination preset) and
// marks the sale reversed. The listing stays sold; money is handled manually.
func (s *Service) AdminReverse(ctx context.Context, actor, saleID uuid.UUID, rawReason string) (Sale, error) {
	if err := s.enabled(); err != nil {
		return Sale{}, err
	}
	r, err := reason(rawReason, true)
	if err != nil {
		return Sale{}, err
	}
	sale, err := s.repo.GetSale(ctx, saleID)
	if err != nil {
		return Sale{}, err
	}
	if sale.Status != SaleCompleted {
		return Sale{}, errNotReversible
	}
	return s.repo.AdminReverseSale(ctx, actor, saleID, r, s.cfg.HoldPeriod)
}

// AdminRestrict bans an account from the market or suspends its offers. A
// ban does not close the account's existing listings or offers: buys from a
// banned seller already fail (NOT_LISTABLE), and the admin delists or
// cancels anything else explicitly.
func (s *Service) AdminRestrict(ctx context.Context, actor, account uuid.UUID, in NewRestriction) (Restriction, error) {
	if err := s.enabled(); err != nil {
		return Restriction{}, err
	}
	if account == uuid.Nil {
		return Restriction{}, shared.ErrValidation.WithMessage("accountId is required")
	}
	if in.Kind != RestrictionBan && in.Kind != RestrictionOfferSuspension {
		return Restriction{}, shared.ErrValidation.WithMessage("kind must be ban or offer_suspension")
	}
	if in.Until != nil && !in.Until.After(s.now()) {
		return Restriction{}, shared.ErrValidation.WithMessage("until must be in the future")
	}
	r, err := reason(in.Reason, true)
	if err != nil {
		return Restriction{}, err
	}
	in.Reason = r
	return s.repo.AdminRestrict(ctx, actor, account, in)
}

// AdminLiftRestriction ends an active restriction now.
func (s *Service) AdminLiftRestriction(ctx context.Context, actor, id uuid.UUID, rawReason string) (Restriction, error) {
	if err := s.enabled(); err != nil {
		return Restriction{}, err
	}
	r, err := reason(rawReason, false)
	if err != nil {
		return Restriction{}, err
	}
	return s.repo.LiftRestriction(ctx, actor, id, r)
}

// AdminRestrictions lists restrictions, newest first: active ones unless
// includeInactive.
func (s *Service) AdminRestrictions(ctx context.Context, account *uuid.UUID, includeInactive bool, limit int) ([]Restriction, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	f, err := AdminFilter{Limit: limit}.normalize(nil)
	if err != nil {
		return nil, err
	}
	return s.repo.AdminRestrictions(ctx, account, includeInactive, f.Limit)
}

// restrictionActive mirrors the SQL definition of an active restriction.
func restrictionActive(r Restriction, now time.Time) bool {
	return r.LiftedAt == nil && (r.Until == nil || r.Until.After(now))
}
