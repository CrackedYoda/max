package market

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/market/settle"
)

// Fake repository: worker queries. The fake clock (fakeRepo.clock) stands in
// for the database's now().

type fakeEvent struct {
	saleID   uuid.UUID
	kind     string
	from, to string
}

func (f *fakeRepo) now() time.Time {
	if f.clock != nil {
		return f.clock()
	}
	return time.Now()
}

func (f *fakeRepo) record(id uuid.UUID, kind, from, to string) {
	f.events = append(f.events, fakeEvent{id, kind, from, to})
}

func (f *fakeRepo) eventsOf(id uuid.UUID, kind string) int {
	n := 0
	for _, e := range f.events {
		if e.saleID == id && e.kind == kind {
			n++
		}
	}
	return n
}

func (f *fakeRepo) ClaimDueSales(_ context.Context, limit int, lease time.Duration) ([]Sale, error) {
	var out []Sale
	now := f.now()
	for id, s := range f.sales {
		if observable(s) && s.NextCheckAt != nil && !s.NextCheckAt.After(now) && len(out) < limit {
			next := now.Add(lease)
			s.NextCheckAt = &next
			f.sales[id] = s
			out = append(out, s)
		}
	}
	return out, nil
}

// observable mirrors ClaimDueMarketSales: open paid sales, and transactions
// submitted after an admin cancel still waiting for a verdict.
func observable(s Sale) bool {
	return s.Status == SaleSubmitted || s.Status == SaleConfirming ||
		(s.Status == SaleCancelled && s.TxHash != nil && s.NextCheckAt != nil)
}

func (f *fakeRepo) ClaimSalesPastDeadline(_ context.Context, cutoff time.Time, limit int, lease time.Duration) ([]Sale, error) {
	var out []Sale
	now := f.now()
	for id, s := range f.sales {
		if (s.Status == SaleSubmitted || s.Status == SaleConfirming) && !txDeadline(s).Add(-txDeadlineGrace).After(cutoff) &&
			(s.NextCheckAt == nil || !s.NextCheckAt.After(now)) && len(out) < limit {
			next := now.Add(lease)
			s.NextCheckAt = &next
			f.sales[id] = s
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeRepo) ExpiredLocks(_ context.Context, cutoff time.Time, limit int) ([]Sale, error) {
	var out []Sale
	for _, s := range f.sales {
		if s.Status == SaleLocked && s.LockExpiresAt != nil && !s.LockExpiresAt.After(cutoff) && len(out) < limit {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeRepo) UnlapsedOffers(_ context.Context, limit int) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for _, s := range f.sales {
		if s.OfferID != nil && s.Status == SaleExpired && s.TxHash == nil && f.offers[*s.OfferID].Status == OfferAccepted && len(out) < limit {
			out = append(out, *s.OfferID)
		}
	}
	return out, nil
}

func (f *fakeRepo) AttachLateTx(_ context.Context, id, buyer uuid.UUID, h string) (Sale, error) {
	s := f.sales[id]
	if s.BuyerAccountID != buyer {
		return Sale{}, ErrSaleNotFound
	}
	if s.Status != SaleExpired || s.TxHash != nil {
		return Sale{}, ErrSaleExpired
	}
	to := SaleConfirming
	if f.owner != s.SellerAccountID {
		to = SaleManualReview
	}
	for _, other := range f.sales {
		if other.ID != id && other.MaxID == s.MaxID && IsOpenSale(other.Status) {
			to = SaleManualReview
		}
	}
	now := f.now()
	s.Status, s.TxHash, s.SubmittedAt = to, &h, &now
	s.NextCheckAt = nil
	if to == SaleConfirming {
		s.NextCheckAt = &now
		if s.ListingID != nil {
			if l := f.listings[*s.ListingID]; l.Status == ListingActive {
				l.Status = ListingLocked
				f.listings[l.ID] = l
			}
		}
	}
	f.sales[id] = s
	f.record(id, "late_submitted", SaleExpired, to)
	return s, nil
}

func (f *fakeRepo) LeaseSale(_ context.Context, id uuid.UUID, lease time.Duration) (bool, error) {
	s, ok := f.sales[id]
	if !ok || !observable(s) {
		return false, nil
	}
	next := f.now().Add(lease)
	s.NextCheckAt = &next
	f.sales[id] = s
	f.leases++
	return true, nil
}

func (f *fakeRepo) ExpireListings(_ context.Context, cutoff time.Time, limit int) (int, error) {
	n := 0
	for id, l := range f.listings {
		if l.Status == ListingActive && l.ExpiresAt != nil && !l.ExpiresAt.After(cutoff) && n < limit {
			l.Status, l.StatusReason = ListingCancelled, "expired"
			f.listings[id] = l
			n++
		}
	}
	return n, nil
}

// worker harness helpers

func (h *harness) at(t time.Time) {
	h.svc.now = func() time.Time { return t }
	h.repo.clock = func() time.Time { return t }
}

func (h *harness) tick(t *testing.T) Result {
	t.Helper()
	res, err := h.svc.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	return res
}

func (h *harness) boughtAndSubmitted(t *testing.T) Sale {
	t.Helper()
	v, err := h.buy("buy-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.svc.Submit(context.Background(), human(h.buyer), v.Sale.ID, validSig())
	if err != nil {
		t.Fatal(err)
	}
	return out.Sale
}

var (
	obsMatch    = settle.Observation{Status: settle.StatusConfirmed, Matches: true, PaidTowardSale: true, Confirmations: 1}
	obsGarbage  = settle.Observation{Status: settle.StatusConfirmed, Reason: "no transferChecked of the mint to the treasury account", Confirmations: 1}
	obsPaidBad  = settle.Observation{Status: settle.StatusConfirmed, Reason: "seller transfer amount does not equal the seller amount", PaidTowardSale: true, Confirmations: 1}
	obsNotFound = settle.Observation{Status: settle.StatusNotFound, Reason: "transaction not found"}
	obsPending  = settle.Observation{Status: settle.StatusPending, Reason: "transaction not finalized"}
)

func TestTick_CompletesOnMatch(t *testing.T) {
	h := newHarness(t, nil)
	s := h.boughtAndSubmitted(t)
	h.adapter.obs = obsMatch
	res := h.tick(t)
	if res.Completed != 1 || h.repo.sales[s.ID].Status != SaleCompleted {
		t.Fatalf("res %+v sale %s", res, h.repo.sales[s.ID].Status)
	}
	if res := h.tick(t); res != (Result{}) {
		t.Fatalf("second tick did work: %+v", res)
	}
}

func TestTick_MismatchKeepsSaleAndRecordsEvent(t *testing.T) {
	h := newHarness(t, nil)
	start := time.Now()
	h.at(start)
	s := h.boughtAndSubmitted(t)
	h.adapter.obs = obsGarbage
	res := h.tick(t)
	got := h.repo.sales[s.ID]
	if res.ManualReview != 0 || got.Status != SaleSubmitted || h.repo.eventsOf(s.ID, "tx_mismatch") != 1 {
		t.Fatalf("res %+v sale %s events %v", res, got.Status, h.repo.events)
	}
	// A finalized transaction never changes: the next look is at the deadline.
	if got.NextCheckAt == nil || !got.NextCheckAt.Equal(txDeadline(got)) {
		t.Fatalf("next check %v, deadline %v", got.NextCheckAt, txDeadline(got))
	}
	// Past the lock but before the deadline nothing happens.
	h.at(start.Add(h.svc.cfg.BuyLock + time.Minute))
	if res := h.tick(t); res.Expired != 0 || h.repo.sales[s.ID].Status != SaleSubmitted {
		t.Fatalf("before deadline: %+v %s", res, h.repo.sales[s.ID].Status)
	}
	h.at(start.Add(h.svc.cfg.BuyLock + txDeadlineGrace + time.Second))
	res = h.tick(t)
	got = h.repo.sales[s.ID]
	if res.Expired != 1 || got.Status != SaleExpired || h.repo.listings[h.listing].Status != ListingActive {
		t.Fatalf("after deadline: %+v sale %s listing %s", res, got.Status, h.repo.listings[h.listing].Status)
	}
	if h.repo.eventsOf(s.ID, "tx_mismatch") != 1 || h.repo.eventsOf(s.ID, "expired") != 1 {
		t.Fatalf("events %v", h.repo.events)
	}
	if h.repo.expiries[s.ID].Reason != "tx not found or not a payment for this sale" {
		t.Fatalf("reason %q", h.repo.expiries[s.ID].Reason)
	}
}

func TestTick_PaidMismatchGoesToManualReview(t *testing.T) {
	h := newHarness(t, nil)
	s := h.boughtAndSubmitted(t)
	h.adapter.obs = obsPaidBad
	if res := h.tick(t); res.ManualReview != 1 || h.repo.sales[s.ID].Status != SaleManualReview {
		t.Fatalf("res %+v sale %s", res, h.repo.sales[s.ID].Status)
	}
}

func TestTick_NotFoundBacksOffThenExpiresAtDeadline(t *testing.T) {
	h := newHarness(t, nil)
	start := time.Now()
	h.at(start)
	s := h.boughtAndSubmitted(t)
	h.adapter.obs = obsNotFound
	h.tick(t)
	got := h.repo.sales[s.ID]
	if got.Status != SaleSubmitted || got.NextCheckAt == nil || !got.NextCheckAt.After(start) || len(h.repo.events) != 2 {
		t.Fatalf("first look: %s next %v events %v", got.Status, got.NextCheckAt, h.repo.events)
	}
	// Rechecks never land past the deadline.
	h.at(txDeadline(got).Add(-time.Second))
	h.repo.sales[s.ID] = withNextCheck(got, txDeadline(got).Add(-time.Second))
	h.tick(t)
	if next := h.repo.sales[s.ID].NextCheckAt; next == nil || next.After(txDeadline(got)) {
		t.Fatalf("next check %v past deadline %v", next, txDeadline(got))
	}
	h.at(txDeadline(got))
	res := h.tick(t)
	if res.Expired != 1 || h.repo.sales[s.ID].Status != SaleExpired || h.repo.listings[h.listing].Status != ListingActive {
		t.Fatalf("at deadline: %+v %s", res, h.repo.sales[s.ID].Status)
	}
}

func withNextCheck(s Sale, at time.Time) Sale {
	s.NextCheckAt = &at
	return s
}

func TestRecheckNotFound_BacksOff(t *testing.T) {
	prev := time.Duration(0)
	for _, waited := range []time.Duration{0, time.Minute, 5 * time.Minute, time.Hour} {
		d := recheckNotFound(waited)
		if d < prev || d < 10*time.Second || d > 2*time.Minute {
			t.Fatalf("recheckNotFound(%v) = %v after %v", waited, d, prev)
		}
		prev = d
	}
}

func TestTick_ExpiresUnpaidLock(t *testing.T) {
	h := newHarness(t, nil)
	start := time.Now()
	h.at(start)
	v, err := h.buy("buy-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	if res := h.tick(t); res.Expired != 0 {
		t.Fatalf("expired inside the lock: %+v", res)
	}
	h.at(start.Add(h.svc.cfg.BuyLock))
	res := h.tick(t)
	if res.Expired != 1 || h.repo.sales[v.Sale.ID].Status != SaleExpired || h.repo.listings[h.listing].Status != ListingActive {
		t.Fatalf("res %+v sale %s listing %s", res, h.repo.sales[v.Sale.ID].Status, h.repo.listings[h.listing].Status)
	}
	if h.repo.eventsOf(v.Sale.ID, "expired") != 1 {
		t.Fatalf("events %v", h.repo.events)
	}
}

func TestTick_SubmittedPastLockStillCompletes(t *testing.T) {
	h := newHarness(t, nil)
	start := time.Now()
	h.at(start)
	s := h.boughtAndSubmitted(t)
	h.adapter.obs = obsPending
	h.tick(t)
	if h.repo.sales[s.ID].Status != SaleConfirming {
		t.Fatalf("pending: %s", h.repo.sales[s.ID].Status)
	}
	// Still pending well past the lock and the deadline: a seen transaction
	// is kept until it is final or provably failed.
	h.at(start.Add(h.svc.cfg.BuyLock + 2*txDeadlineGrace))
	if res := h.tick(t); res.Expired != 0 || h.repo.sales[s.ID].Status != SaleConfirming {
		t.Fatalf("pending past deadline: %+v %s", res, h.repo.sales[s.ID].Status)
	}
	h.adapter.obs = obsMatch
	h.at(start.Add(h.svc.cfg.BuyLock + 3*txDeadlineGrace))
	if res := h.tick(t); res.Completed != 1 || h.repo.sales[s.ID].Status != SaleCompleted {
		t.Fatalf("final: %+v %s", res, h.repo.sales[s.ID].Status)
	}
}

func (h *harness) expiredUnpaid(t *testing.T) Sale {
	t.Helper()
	start := time.Now()
	h.at(start)
	v, err := h.buy("buy-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	h.at(start.Add(h.svc.cfg.BuyLock + time.Minute))
	if res := h.tick(t); res.Expired != 1 {
		t.Fatalf("expire: %+v", res)
	}
	return v.Sale
}

func TestTick_LateValidTxAfterExpiry_Completes(t *testing.T) {
	h := newHarness(t, nil)
	s := h.expiredUnpaid(t)
	out, err := h.svc.Submit(context.Background(), human(h.buyer), s.ID, validSig())
	if err != nil || out.Sale.Status != SaleConfirming {
		t.Fatalf("late submit: %v %+v", err, out.Sale)
	}
	if h.repo.listings[h.listing].Status != ListingLocked {
		t.Fatalf("listing %s, want locked while the late sale is open", h.repo.listings[h.listing].Status)
	}
	h.adapter.obs = obsMatch
	if res := h.tick(t); res.Completed != 1 || h.repo.sales[s.ID].Status != SaleCompleted {
		t.Fatalf("res %+v sale %s", res, h.repo.sales[s.ID].Status)
	}
}

func TestTick_LateValidTxAfterResale_ManualReview(t *testing.T) {
	h := newHarness(t, nil)
	s := h.expiredUnpaid(t)
	h.repo.owner = uuid.New() // the name changed hands
	out, err := h.svc.Submit(context.Background(), human(h.buyer), s.ID, validSig())
	if err != nil || out.Sale.Status != SaleManualReview || out.Sale.TxHash == nil {
		t.Fatalf("late submit: %v %+v", err, out.Sale)
	}
	h.adapter.obs = obsMatch
	if res := h.tick(t); res.Completed != 0 || h.repo.sales[s.ID].Status != SaleManualReview {
		t.Fatalf("res %+v sale %s", res, h.repo.sales[s.ID].Status)
	}
}

func TestSubmit_LateOnlyForUnpaidExpiry(t *testing.T) {
	h := newHarness(t, nil)
	s := h.boughtAndSubmitted(t)
	h.adapter.obs = settle.Observation{Status: settle.StatusFailed, Reason: "transaction failed on chain"}
	h.tick(t)
	if h.repo.sales[s.ID].Status != SaleExpired {
		t.Fatalf("failed tx: %s", h.repo.sales[s.ID].Status)
	}
	if _, err := h.svc.Submit(context.Background(), human(h.buyer), s.ID, validSig()); !errors.Is(err, ErrTxMismatch) {
		t.Fatalf("second tx on an expired paid sale: %v", err)
	}
	// Only a sale an admin cancelled after it was locked takes a
	// transaction late: it is recorded, the sale stays cancelled and the
	// worker judges it. Other closed sales refuse.
	c := h.repo.sales[s.ID]
	c.Status, c.TxHash = SaleCancelled, nil
	h.repo.sales[s.ID] = c
	if v, err := h.svc.Submit(context.Background(), human(h.buyer), s.ID, validSig()); err != nil || v.Sale.Status != SaleCancelled || v.Sale.TxHash == nil {
		t.Fatalf("cancelled sale: %+v %v", v.Sale, err)
	}
	for _, st := range []string{SaleReversed, SaleManualReview, SaleCompleted} {
		c.Status, c.TxHash = st, nil
		h.repo.sales[s.ID] = c
		if _, err := h.svc.Submit(context.Background(), human(h.buyer), s.ID, validSig()); !errors.Is(err, ErrSaleExpired) {
			t.Fatalf("%s sale: %v", st, err)
		}
	}
}

func TestTick_ExpiredOfferLapsesWithStrike(t *testing.T) {
	h := newHarness(t, nil)
	start := time.Now()
	h.at(start)
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	v, err := h.accept(o.ID, h.payout)
	if err != nil {
		t.Fatal(err)
	}
	h.at(start.Add(h.svc.cfg.OfferPayWindow))
	res := h.tick(t)
	if res.Expired != 1 || h.repo.sales[v.Sale.ID].Status != SaleExpired {
		t.Fatalf("res %+v sale %s", res, h.repo.sales[v.Sale.ID].Status)
	}
	if h.repo.offers[o.ID].Status != OfferLapsed || len(h.repo.strikes[h.buyer]) != 1 {
		t.Fatalf("offer %s strikes %d", h.repo.offers[o.ID].Status, len(h.repo.strikes[h.buyer]))
	}
	if h.tick(t); len(h.repo.strikes[h.buyer]) != 1 {
		t.Fatalf("second tick added a strike")
	}
}

func TestTick_RepairsUnlapsedOffer(t *testing.T) {
	h := newHarness(t, nil)
	o := h.seedOffer(h.buyer, h.payer, OfferAccepted)
	h.seedOfferSale(o, SaleExpired, nil) // the worker stopped between expiry and lapse
	h.tick(t)
	if h.repo.offers[o.ID].Status != OfferLapsed || len(h.repo.strikes[h.buyer]) != 1 {
		t.Fatalf("offer %s strikes %d", h.repo.offers[o.ID].Status, len(h.repo.strikes[h.buyer]))
	}
}

func TestTick_SweepsExpiredListingsAndRechecksFunding(t *testing.T) {
	h := newHarness(t, nil)
	past := time.Now().Add(-time.Minute)
	l := h.repo.listings[h.listing]
	l.ExpiresAt = &past
	h.repo.listings[h.listing] = l
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	h.adapter.fundsErr = shortfall()
	res := h.tick(t)
	if res.ListingsExpired != 1 || h.repo.listings[h.listing].Status != ListingCancelled || h.repo.listings[h.listing].StatusReason != "expired" {
		t.Fatalf("res %+v listing %+v", res, h.repo.listings[h.listing])
	}
	if res.FundingChecked != 1 || h.repo.offers[o.ID].Status != OfferUnfunded {
		t.Fatalf("res %+v offer %s", res, h.repo.offers[o.ID].Status)
	}
}

func TestTick_DisabledIsIdle(t *testing.T) {
	h := newHarness(t, nil)
	s := h.boughtAndSubmitted(t)
	h.svc.cfg.Enabled = false
	h.adapter.obs = obsMatch
	if res := h.tick(t); res != (Result{}) || h.repo.sales[s.ID].Status != SaleSubmitted {
		t.Fatalf("disabled tick: %+v %s", res, h.repo.sales[s.ID].Status)
	}
}

func TestRunWorker_StopsOnCancel(t *testing.T) {
	h := newHarness(t, nil)
	s := h.boughtAndSubmitted(t)
	h.adapter.obs = obsMatch
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		RunWorker(ctx, h.svc, 5*time.Millisecond, zap.NewNop())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop")
	}
	if h.repo.sales[s.ID].Status != SaleCompleted {
		t.Fatalf("worker never completed the sale: %s", h.repo.sales[s.ID].Status)
	}
}

func TestObserveSale_MismatchEventOnce(t *testing.T) {
	h := newHarness(t, nil)
	s := h.boughtAndSubmitted(t)
	h.adapter.obs = obsGarbage
	for i := 0; i < 3; i++ {
		if _, err := h.svc.ObserveSale(context.Background(), s.ID); err != nil {
			t.Fatal(err)
		}
	}
	if n := h.repo.eventsOf(s.ID, "tx_mismatch"); n != 1 || h.repo.sales[s.ID].Status != SaleSubmitted {
		t.Fatalf("tx_mismatch events %d, status %s", n, h.repo.sales[s.ID].Status)
	}
}

// A hung RPC on one sale must not hold up lock expiry or the sweep: the
// observe step stops at its budget and leaves the rest for the next tick.
func TestTick_SlowObserveStillExpiresLocks(t *testing.T) {
	h := newHarness(t, nil)
	start := time.Now()
	h.at(start)
	v, err := h.buy("buy-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ { // submitted sales on other names whose RPC hangs
		slow := v.Sale
		tx, lock := validSig(), start.Add(time.Hour)
		slow.ID, slow.MaxID, slow.ListingID, slow.Status = uuid.New(), uuid.New(), nil, SaleSubmitted
		slow.TxHash, slow.SubmittedAt, slow.NextCheckAt, slow.LockExpiresAt = &tx, &start, &start, &lock
		h.repo.sales[slow.ID] = slow
	}
	h.adapter.delay = time.Hour
	h.svc.observeBudget = 50 * time.Millisecond
	h.at(start.Add(h.svc.cfg.BuyLock))
	began := time.Now()
	res := h.tick(t)
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("tick took %v", took)
	}
	if res.Expired != 1 || h.repo.sales[v.Sale.ID].Status != SaleExpired {
		t.Fatalf("lock not expired: %+v %s", res, h.repo.sales[v.Sale.ID].Status)
	}
	if h.adapter.observed != 1 {
		t.Fatalf("observed %d sales; the rest should wait for the next tick", h.adapter.observed)
	}
	if h.repo.leases != 1 {
		t.Fatalf("re-leased %d sales, want only the one observed", h.repo.leases)
	}
}
