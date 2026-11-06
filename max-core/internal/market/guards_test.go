package market

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/market/settle"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

// Final-review guards: the open-sale cap, the buy cooldown, review timeout,
// payout re-checks, the MAX ID cap, paused closes and payments after an
// admin cancel.

func details(err error) map[string]any {
	var app *shared.AppError
	if errors.As(err, &app) {
		return app.Details
	}
	return nil
}

// otherOpenSale stores an open sale of buyer on some other name.
func (h *harness) otherOpenSale(buyer uuid.UUID, status string) {
	s := Sale{ID: uuid.New(), MaxID: uuid.New(), SellerAccountID: uuid.New(), BuyerAccountID: buyer, Status: status}
	h.repo.sales[s.ID] = s
}

func TestBuy_OpenSaleCapIsTwo(t *testing.T) {
	h := newHarness(t, nil)
	h.otherOpenSale(h.buyer, SaleLocked)
	h.otherOpenSale(h.buyer, SalePendingReview)
	_, err := h.buy("buy-key-cap-1")
	if code(err) != "SALE_LIMIT" || details(err)["reason"] != "open_sales" {
		t.Fatalf("third open sale: %v %v", err, details(err))
	}
	if h.repo.opened != 0 {
		t.Fatal("sale opened past the cap")
	}
	// A finished sale frees a slot.
	for id, s := range h.repo.sales {
		if s.Status == SaleLocked {
			s.Status = SaleCompleted
			h.repo.sales[id] = s
		}
	}
	if _, err := h.buy("buy-key-cap-2"); err != nil {
		t.Fatalf("after one finished: %v", err)
	}
}

func TestAccept_BuyerAtOpenSaleCap(t *testing.T) {
	h := newHarness(t, nil)
	h.otherOpenSale(h.buyer, SaleSubmitted)
	h.otherOpenSale(h.buyer, SaleConfirming)
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	_, err := h.accept(o.ID, h.payout)
	if code(err) != "SALE_LIMIT" || h.repo.offers[o.ID].Status != OfferLive {
		t.Fatalf("accept for a capped buyer: %v offer %s", err, h.repo.offers[o.ID].Status)
	}
}

func TestBuy_CooldownAfterUnpaidExpiry(t *testing.T) {
	h := newHarness(t, nil)
	s := h.expiredUnpaid(t)
	if e := h.repo.expiries[s.ID]; e.Cooldown != buyCooldown {
		t.Fatalf("expiry cooldown %v", e.Cooldown)
	}
	expiredAt := h.svc.now()
	_, err := h.buy("buy-key-again")
	if code(err) != "SALE_LIMIT" || details(err)["reason"] != "buy_cooldown" {
		t.Fatalf("buy during cooldown: %v %v", err, details(err))
	}
	h.at(expiredAt.Add(buyCooldown + time.Second))
	if _, err := h.buy("buy-key-after"); err != nil {
		t.Fatalf("after the cooldown: %v", err)
	}
}

func TestBuy_CooldownOnlyForTheSameListing(t *testing.T) {
	h := newHarness(t, nil)
	h.expiredUnpaid(t)
	other, otherPayer := uuid.New(), solAddr()
	h.repo.link(other, otherPayer)
	if _, err := h.svc.Buy(context.Background(), human(other), h.listing, BuyInput{FromAddress: otherPayer, IdempotencyKey: "other-buyer-1"}); err != nil {
		t.Fatalf("another buyer: %v", err)
	}
}

func TestTick_ReviewTimesOutAfter24h(t *testing.T) {
	h := newHarness(t, nil)
	start := time.Now()
	h.at(start)
	o := h.seedOffer(h.buyer, h.payer, OfferAccepted)
	buy := h.seedSale(SalePendingReview, nil, false)
	buy.CreatedAt = start
	h.repo.sales[buy.ID] = buy
	offerSale := h.seedSale(SalePendingReview, &o, false)
	offerSale.MaxID, offerSale.CreatedAt = uuid.New(), start.Add(time.Hour)
	h.repo.sales[offerSale.ID] = offerSale

	h.at(start.Add(reviewTimeout - time.Minute))
	if res := h.tick(t); res.ReviewsTimedOut != 0 {
		t.Fatalf("timed out early: %+v", res)
	}
	h.at(start.Add(reviewTimeout + time.Minute))
	if res := h.tick(t); res.ReviewsTimedOut != 1 {
		t.Fatalf("first timeout: %+v", res)
	}
	got := h.repo.sales[buy.ID]
	if got.Status != SaleCancelled || got.StatusReason != reasonReviewTimedOut || h.repo.listings[h.listing].Status != ListingActive {
		t.Fatalf("buy sale %s %q listing %s", got.Status, got.StatusReason, h.repo.listings[h.listing].Status)
	}
	h.at(start.Add(reviewTimeout + time.Hour + time.Minute))
	if res := h.tick(t); res.ReviewsTimedOut != 1 {
		t.Fatalf("second timeout: %+v", res)
	}
	if h.repo.offers[o.ID].Status != OfferDeclined || len(h.repo.strikes[h.buyer]) != 0 {
		t.Fatalf("offer %s strikes %d", h.repo.offers[o.ID].Status, len(h.repo.strikes[h.buyer]))
	}
	if h.repo.eventsOf(buy.ID, "review_timed_out") != 1 {
		t.Fatal("no review_timed_out event")
	}
}

func TestBuy_PayoutWalletRemoved(t *testing.T) {
	h := newHarness(t, nil)
	delete(h.repo.wallets, h.seller.String()+"|"+h.payout)
	_, err := h.buy("buy-key-payout")
	if code(err) != "NOT_LISTABLE" || details(err)["reason"] != ReasonUnavailable {
		t.Fatalf("buyer sees %v %v", err, details(err))
	}
}

func TestAccept_ListingPayoutWalletRemoved(t *testing.T) {
	h := newHarness(t, nil)
	l := h.repo.listings[h.listing]
	h.repo.liveListing = &l
	delete(h.repo.wallets, h.seller.String()+"|"+h.payout)
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	_, err := h.accept(o.ID, "")
	if code(err) != "NOT_LISTABLE" || details(err)["reason"] != ReasonPayoutWalletRemoved {
		t.Fatalf("seller sees %v %v", err, details(err))
	}
}

func TestBuy_MasksModerationReasons(t *testing.T) {
	h := newHarness(t, nil)
	h.repo.facts = NameFacts{Status: "active", OwnerStatus: "active", OwnerBanned: true}
	_, err := h.buy("buy-key-banned")
	if code(err) != "NOT_LISTABLE" || details(err)["reason"] != ReasonUnavailable {
		t.Fatalf("got %v %v", err, details(err))
	}
}

func TestMaxIDCap_BuyOfferAccept(t *testing.T) {
	h := newHarness(t, nil)
	h.repo.ownedIDs[h.buyer] = maxids.MaxIDsPerAccount
	if _, err := h.buy("buy-key-ids"); !errors.Is(err, maxids.ErrIDLimitReached) {
		t.Fatalf("buy at the cap: %v", err)
	}
	if _, err := h.offer("20", nil); !errors.Is(err, maxids.ErrIDLimitReached) {
		t.Fatalf("offer at the cap: %v", err)
	}
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	if _, err := h.accept(o.ID, h.payout); !errors.Is(err, maxids.ErrIDLimitReached) {
		t.Fatalf("accept for a buyer at the cap: %v", err)
	}
	// Purchases in progress count: 9 owned + 1 open sale leaves no room.
	h.repo.ownedIDs[h.buyer] = maxids.MaxIDsPerAccount - 1
	h.otherOpenSale(h.buyer, SaleLocked)
	if _, err := h.buy("buy-key-ids-2"); !errors.Is(err, maxids.ErrIDLimitReached) {
		t.Fatalf("buy with a purchase in progress: %v", err)
	}
	h.repo.ownedIDs[h.buyer] = maxids.MaxIDsPerAccount - 2
	if _, err := h.buy("buy-key-ids-3"); err != nil {
		t.Fatalf("buy with room: %v", err)
	}
}

func TestPaused_ClosesStillWork(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	l, err := h.list("25")
	if err != nil {
		t.Fatal(err)
	}
	mine := h.seedOffer(h.buyer, h.payer, OfferLive)
	theirs := h.seedOffer(uuid.New(), solAddr(), OfferLive)
	h.svc.cfg.TradingPaused = true
	if _, err := h.svc.UpdatePrice(ctx, human(h.seller), l.ID, "30"); !errors.Is(err, ErrMarketPaused) {
		t.Fatalf("price update while paused: %v", err)
	}
	if err := h.svc.Delist(ctx, human(h.seller), l.ID); err != nil {
		t.Fatalf("delist while paused: %v", err)
	}
	if err := h.svc.WithdrawOffer(ctx, human(h.buyer), mine.ID); err != nil {
		t.Fatalf("withdraw while paused: %v", err)
	}
	if err := h.svc.DeclineOffer(ctx, human(h.seller), theirs.ID, ""); err != nil {
		t.Fatalf("decline while paused: %v", err)
	}
	if _, err := h.offer("20", nil); !errors.Is(err, ErrMarketPaused) {
		t.Fatalf("new offer while paused: %v", err)
	}
}

// cancelledAfterLock is a buy-now sale an admin cancelled while it was locked.
func (h *harness) cancelledAfterLock(t *testing.T) Sale {
	t.Helper()
	start := time.Now()
	h.at(start)
	v, err := h.buy("buy-key-cancel")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AdminCancel(context.Background(), adminID, v.Sale.ID, "fraud check"); err != nil {
		t.Fatal(err)
	}
	return h.repo.sales[v.Sale.ID]
}

func TestTxAfterCancel_PaidGoesToManualReview(t *testing.T) {
	h := newHarness(t, nil)
	s := h.cancelledAfterLock(t)
	v, err := h.svc.Submit(context.Background(), human(h.buyer), s.ID, validSig())
	if err != nil || v.Sale.Status != SaleCancelled || v.Sale.TxHash == nil || v.Sale.NextCheckAt == nil {
		t.Fatalf("submit after cancel: %v %+v", err, v.Sale)
	}
	h.adapter.obs = obsPending
	h.tick(t)
	if got := h.repo.sales[s.ID]; got.Status != SaleCancelled || got.NextCheckAt == nil {
		t.Fatalf("pending: %s", got.Status)
	}
	h.at(h.svc.now().Add(time.Minute))
	h.adapter.obs = obsPaidBad
	if res := h.tick(t); res.ManualReview != 1 {
		t.Fatalf("res %+v", res)
	}
	if got := h.repo.sales[s.ID]; got.Status != SaleManualReview || h.repo.eventsOf(s.ID, "tx_after_cancel") != 1 {
		t.Fatalf("paid: %s", got.Status)
	}
}

func TestTxAfterCancel_UnverifiedStaysCancelled(t *testing.T) {
	for _, name := range []string{"not found", "not a payment"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, nil)
			s := h.cancelledAfterLock(t)
			if _, err := h.svc.Submit(context.Background(), human(h.buyer), s.ID, validSig()); err != nil {
				t.Fatal(err)
			}
			h.adapter.obs = obsNotFound
			if name == "not a payment" {
				h.adapter.obs = obsGarbage
			}
			h.tick(t)
			if got := h.repo.sales[s.ID]; got.Status != SaleCancelled || got.NextCheckAt == nil {
				t.Fatalf("before the deadline: %s next %v", got.Status, got.NextCheckAt)
			}
			h.at(txDeadline(h.repo.sales[s.ID]).Add(time.Second))
			h.tick(t)
			got := h.repo.sales[s.ID]
			if got.Status != SaleCancelled || got.NextCheckAt != nil || h.repo.eventsOf(s.ID, "tx_after_cancel_unverified") != 1 {
				t.Fatalf("after the deadline: %s next %v", got.Status, got.NextCheckAt)
			}
			// Settled: the worker looks no more.
			observed := h.adapter.observed
			h.at(h.svc.now().Add(time.Hour))
			h.tick(t)
			if h.adapter.observed != observed {
				t.Fatal("observed a settled cancelled sale again")
			}
		})
	}
}

func TestTxAfterCancel_FailedTxStaysCancelled(t *testing.T) {
	h := newHarness(t, nil)
	s := h.cancelledAfterLock(t)
	if _, err := h.svc.Submit(context.Background(), human(h.buyer), s.ID, validSig()); err != nil {
		t.Fatal(err)
	}
	h.adapter.obs = settle.Observation{Status: settle.StatusFailed, Reason: "transaction failed on chain"}
	h.tick(t)
	if got := h.repo.sales[s.ID]; got.Status != SaleCancelled || got.NextCheckAt != nil || h.repo.eventsOf(s.ID, "tx_after_cancel_unverified") != 1 {
		t.Fatalf("failed: %s", got.Status)
	}
}

func TestTxAfterCancel_DeniedReviewRefuses(t *testing.T) {
	h := newHarness(t, nil)
	s := h.seedSale(SalePendingReview, nil, false)
	if _, err := h.svc.AdminDeny(context.Background(), adminID, s.ID, "too large"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Submit(context.Background(), human(h.buyer), s.ID, validSig()); !errors.Is(err, ErrSaleExpired) {
		t.Fatalf("tx for a denied review: %v", err)
	}
	if h.repo.sales[s.ID].TxHash != nil {
		t.Fatal("tx recorded")
	}
}
