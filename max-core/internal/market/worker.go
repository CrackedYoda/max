package market

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Worker timings.
const (
	// txDeadlineGrace is how long a submitted transaction may stay unseen, or
	// seen but not a payment for the sale, after the later of the lock end and
	// its submission before the sale expires. A Solana transaction whose
	// blockhash expired can no longer land, so 30 minutes is conclusive.
	txDeadlineGrace = 30 * time.Minute
	// recheckPending re-checks a seen transaction until it is finalized.
	recheckPending = 10 * time.Second
	// observeLease holds a claimed sale away from other workers while one
	// observes it; it is also the retry delay after an RPC failure.
	observeLease = 30 * time.Second
	// observeTimeout bounds one sale's RPC calls.
	observeTimeout = 20 * time.Second
	tickBatch      = 100
	fundingBatch   = 100
	// reasonTxNotFound is the expiry reason once the deadline passes.
	reasonTxNotFound = "tx not found or not a payment for this sale"
	reasonLockEnded  = "payment window ended without a transaction"
)

// recheckNotFound backs off re-checks of a transaction not yet visible,
// like the payments worker: quick at first, then every two minutes.
func recheckNotFound(waited time.Duration) time.Duration {
	switch {
	case waited < 2*time.Minute:
		return 15 * time.Second
	case waited < 10*time.Minute:
		return 30 * time.Second
	}
	return 2 * time.Minute
}

// txDeadline is when a submitted sale whose transaction is not found, or is
// final but no payment for the sale, expires: txDeadlineGrace after the later
// of the lock end and the submission. Counting from the submission gives a
// late payment, submitted after the sale expired, the same grace.
// created_at stands in only when both are missing. Mirrors the
// ClaimMarketSalesPastTxDeadline query.
func txDeadline(s Sale) time.Time {
	var anchor *time.Time
	for _, t := range []*time.Time{s.LockExpiresAt, s.SubmittedAt} {
		if t != nil && (anchor == nil || t.After(*anchor)) {
			anchor = t
		}
	}
	if anchor == nil {
		anchor = &s.CreatedAt
	}
	return anchor.Add(txDeadlineGrace)
}

// Result counts what one Tick did. Completed, Expired and ManualReview count
// sales; FundingChecked counts offers whose funding status changed (offer
// expiries included, as RecheckFunding reports them).
type Result struct {
	Completed, Expired, ManualReview, FundingChecked int
	ListingsExpired                                  int
}

func (r Result) empty() bool { return r == Result{} }

// Tick runs one pass of the market worker. Steps, in order:
//
//  1. Observe submitted and confirming sales due by next_check_at.
//  2. Expire locked sales past their lock with no transaction; an expired
//     offer sale lapses its offer with a strike.
//  3. Apply the transaction deadline to submitted and confirming sales no
//     check is scheduled for (observing once more first).
//  4. Close active listings past their expiry.
//  5. Re-check offer funding.
//
// A failure on one sale is logged and the pass goes on; failures to read a
// step's work are returned joined. With the market disabled Tick does nothing.
func (s *Service) Tick(ctx context.Context) (Result, error) {
	var res Result
	if !s.cfg.Enabled {
		return res, nil
	}
	var errs []error

	if due, err := s.repo.ClaimDueSales(ctx, tickBatch, observeLease); err != nil {
		errs = append(errs, err)
	} else {
		for _, sale := range due {
			s.observe(ctx, sale.ID, &res)
		}
	}

	if err := s.expireLocks(ctx, &res); err != nil {
		errs = append(errs, err)
	}

	if past, err := s.repo.ClaimSalesPastDeadline(ctx, s.now().Add(-txDeadlineGrace), tickBatch, observeLease); err != nil {
		errs = append(errs, err)
	} else {
		for _, sale := range past {
			s.observe(ctx, sale.ID, &res)
		}
	}

	if n, err := s.repo.ExpireListings(ctx, s.now(), tickBatch); err != nil {
		errs = append(errs, err)
	} else {
		res.ListingsExpired = n
	}

	if n, err := s.RecheckFunding(ctx, fundingBatch); err != nil {
		errs = append(errs, err)
	} else {
		res.FundingChecked = n
	}
	return res, errors.Join(errs...)
}

// observe runs ObserveSale on one claimed sale and counts the outcome.
func (s *Service) observe(ctx context.Context, id uuid.UUID, res *Result) {
	octx, cancel := context.WithTimeout(ctx, observeTimeout)
	defer cancel()
	out, err := s.ObserveSale(octx, id)
	if err != nil {
		// The claim's lease is the retry delay.
		s.log.Warn("market observe sale", zap.String("sale_id", id.String()), zap.Error(err))
		return
	}
	s.count(out.Status, res)
}

func (s *Service) count(status string, res *Result) {
	switch status {
	case SaleCompleted:
		res.Completed++
	case SaleExpired:
		res.Expired++
	case SaleManualReview:
		res.ManualReview++
	}
}

// expireLocks closes unpaid locked sales past their lock (spec §6 Expiry and
// late payment). ExpireSale returns the name's listing to active; an offer
// sale also lapses its offer, which records a strike. It then lapses offers a
// previous pass expired but never lapsed.
func (s *Service) expireLocks(ctx context.Context, res *Result) error {
	now := s.now()
	locked, err := s.repo.ExpiredLocks(ctx, now, tickBatch)
	if err != nil {
		return err
	}
	for _, sale := range locked {
		out, err := s.repo.ExpireSale(ctx, sale.ID, Expiry{From: []string{SaleLocked}, LockedBefore: &now, Reason: reasonLockEnded})
		if err != nil {
			s.log.Warn("market expire sale", zap.String("sale_id", sale.ID.String()), zap.Error(err))
			continue
		}
		if out.Status != SaleExpired {
			continue // paid or moved on meanwhile
		}
		res.Expired++
		if sale.OfferID != nil {
			s.lapse(ctx, *sale.OfferID)
		}
	}
	ids, err := s.repo.UnlapsedOffers(ctx, tickBatch)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.lapse(ctx, id)
	}
	return nil
}

// lapse closes an accepted offer whose sale expired unpaid. errNotLapsable is
// expected: a late payment may have reached the sale first.
func (s *Service) lapse(ctx context.Context, offerID uuid.UUID) {
	err := s.LapseAcceptedOffer(ctx, offerID)
	switch {
	case err == nil:
	case errors.Is(err, errNotLapsable):
		s.log.Debug("market offer not lapsable", zap.String("offer_id", offerID.String()), zap.Error(err))
	default:
		s.log.Warn("market lapse offer", zap.String("offer_id", offerID.String()), zap.Error(err))
	}
}

// RunWorker runs Tick every interval until ctx ends. main starts it only when
// the market is enabled.
func RunWorker(ctx context.Context, s *Service, every time.Duration, log *zap.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			res, err := s.Tick(ctx)
			if err != nil {
				log.Error("market tick", zap.Error(err))
			}
			if !res.empty() {
				log.Info("market tick", zap.Int("completed", res.Completed), zap.Int("expired", res.Expired),
					zap.Int("manual_review", res.ManualReview), zap.Int("funding_changed", res.FundingChecked),
					zap.Int("listings_expired", res.ListingsExpired))
			}
		}
	}
}
