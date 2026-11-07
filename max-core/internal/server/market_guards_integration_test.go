//go:build integration

package server_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/max/max-core/internal/config"
)

// Final-review guards (spec §6, §7): open-sale cap, review timeout, buy
// cooldown, payout re-check, public search filter, MAX ID cap and strike
// removal on cancelling a late-paid lapsed offer.

func errReason(body map[string]any) any {
	if e, ok := body["error"].(map[string]any); ok {
		if d, ok := e["details"].(map[string]any); ok {
			return d["reason"]
		}
	}
	return nil
}

func TestMarketGuards_OpenSaleCapIsTwo(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob := newParty(t, env), newParty(t, env)
	var listings []string
	for _, name := range []string{"yoda", "luke", "leia"} {
		id := claimID(t, alice, name)
		makeListable(t, env, id)
		listings = append(listings, listName(t, alice, id, "25"))
	}
	for i := 0; i < 2; i++ {
		if code, body := buyListing(bob, listings[i], fmt.Sprintf("cap-buy-%04d", i)); code != http.StatusCreated {
			t.Fatalf("buy %d: %d %v", i, code, body)
		}
	}
	code, body := buyListing(bob, listings[2], "cap-buy-0002")
	if code != http.StatusConflict || errCode(body) != "SALE_LIMIT" || errReason(body) != "open_sales" {
		t.Fatalf("third buy: %d %v", code, body)
	}
	if listingStatus(t, env, listings[2]) != "active" {
		t.Fatal("refused buy locked the listing")
	}
	// An accepted offer counts toward the buyer's cap too.
	jedi := claimID(t, alice, "jedi")
	makeListable(t, env, jedi)
	exec(t, env, `UPDATE market_sales SET status = 'expired' WHERE buyer_account_id = $1 AND listing_id::text = $2`, bob.account, listings[1])
	seedOfferSale(t, env, jedi, alice, bob)
	if code, body = buyListing(bob, listings[2], "cap-buy-0003"); errCode(body) != "SALE_LIMIT" {
		t.Fatalf("buy with an open offer sale: %d %v", code, body)
	}
}

func TestMarketGuards_ReviewTimesOut(t *testing.T) {
	env := newTestEnv(t, withMarket(func(c *config.MarketConfig) { c.ReviewThreshold = "100" }))
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "500")
	_, body := buyListing(bob, listingID, "review-buy-0001")
	saleID := saleOf(t, body)["id"].(string)
	if saleStatus(t, env, saleID) != "pending_review" {
		t.Fatalf("not in review: %v", body)
	}
	exec(t, env, `UPDATE market_sales SET created_at = now() - interval '23 hours' WHERE id = $1`, saleID)
	if res := tick(t, env); res.ReviewsTimedOut != 0 {
		t.Fatalf("timed out early: %+v", res)
	}
	exec(t, env, `UPDATE market_sales SET created_at = now() - interval '25 hours' WHERE id = $1`, saleID)
	if res := tick(t, env); res.ReviewsTimedOut != 1 {
		t.Fatalf("tick: %+v", res)
	}
	if saleStatus(t, env, saleID) != "cancelled" || str(t, env, `SELECT status_reason FROM market_sales WHERE id = $1`, saleID) != "review timed out" {
		t.Fatal("sale not cancelled for review timeout")
	}
	if listingStatus(t, env, listingID) != "active" {
		t.Fatal("listing not released")
	}
	if n := count(t, env, `SELECT count(*) FROM market_sale_events WHERE sale_id = $1 AND event_type = 'review_timed_out'`, saleID); n != 1 {
		t.Fatalf("events %d", n)
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action = 'market.sale_review_timed_out' AND resource_id = $1`, saleID); n != 1 {
		t.Fatalf("audit %d", n)
	}
	if n := count(t, env, `SELECT count(*) FROM market_strikes WHERE account_id = $1`, bob.account); n != 0 {
		t.Fatalf("strikes %d", n)
	}

	// An offer sale in review: the offer is declined, no strike.
	luke := claimID(t, alice, "luke")
	makeListable(t, env, luke)
	offerID, offerSale := seedOfferSale(t, env, luke, alice, bob)
	exec(t, env, `UPDATE market_sales SET status = 'pending_review', lock_expires_at = NULL, created_at = now() - interval '25 hours' WHERE id = $1`, offerSale)
	tick(t, env)
	if saleStatus(t, env, offerSale.String()) != "cancelled" ||
		str(t, env, `SELECT status FROM market_offers WHERE id = $1`, offerID) != "declined" ||
		str(t, env, `SELECT decline_note FROM market_offers WHERE id = $1`, offerID) != "review timed out" {
		t.Fatal("offer sale not timed out")
	}
}

func TestMarketGuards_BuyCooldownAfterUnpaidExpiry(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	_, body := buyListing(bob, listingID, "cool-buy-0001")
	saleID := saleOf(t, body)["id"].(string)
	exec(t, env, `UPDATE market_sales SET lock_expires_at = now() - interval '1 minute' WHERE id = $1`, saleID)
	if res := tick(t, env); res.Expired != 1 {
		t.Fatalf("tick: %+v", res)
	}
	if n := count(t, env, `SELECT count(*) FROM market_buy_cooldowns WHERE buyer_account_id = $1 AND listing_id::text = $2
		AND until > now() + interval '59 minutes'`, bob.account, listingID); n != 1 {
		t.Fatalf("cooldowns %d", n)
	}
	code, body := buyListing(bob, listingID, "cool-buy-0002")
	if code != http.StatusConflict || errCode(body) != "SALE_LIMIT" || errReason(body) != "buy_cooldown" {
		t.Fatalf("buy in cooldown: %d %v", code, body)
	}
	exec(t, env, `UPDATE market_buy_cooldowns SET until = now() - interval '1 second'`)
	if code, body = buyListing(bob, listingID, "cool-buy-0003"); code != http.StatusCreated {
		t.Fatalf("buy after cooldown: %d %v", code, body)
	}
}

func TestMarketGuards_PayoutWalletRemoved(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	exec(t, env, `UPDATE wallets SET status = 'removed' WHERE id = $1`, alice.walletID)
	code, body := buyListing(bob, listingID, "payout-buy-0001")
	if code != http.StatusUnprocessableEntity || errCode(body) != "NOT_LISTABLE" || errReason(body) != "unavailable" {
		t.Fatalf("buy with removed payout: %d %v", code, body)
	}
	// The seller accepting an offer is told why.
	offerID, _ := seedOfferSale(t, env, id, alice, bob)
	exec(t, env, `DELETE FROM market_sale_events WHERE sale_id IN (SELECT id FROM market_sales WHERE offer_id = $1)`, offerID)
	exec(t, env, `DELETE FROM market_sales WHERE offer_id = $1`, offerID)
	exec(t, env, `UPDATE market_offers SET status = 'live' WHERE id = $1`, offerID)
	if code, body = alice.c.do("POST", "/v1/market/offers/"+offerID.String()+"/accept", nil); errCode(body) != "NOT_LISTABLE" || errReason(body) != "payout_wallet_removed" {
		t.Fatalf("accept with removed payout: %d %v", code, body)
	}
}

func TestMarketGuards_SearchHidesUnsellable(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, carol, ops := newParty(t, env), newParty(t, env), newAdmin(t, env)
	anon := &client{t: t, h: env.handler}
	for _, p := range []struct {
		who  party
		name string
	}{{alice, "yoda"}, {carol, "luke"}, {carol, "leia"}} {
		id := claimID(t, p.who, p.name)
		makeListable(t, env, id)
		listName(t, p.who, id, "25")
	}
	names := func() map[string]bool {
		_, body := anon.do("GET", "/v1/market/listings", nil)
		out := map[string]bool{}
		for _, l := range data(t, body)["listings"].([]any) {
			out[l.(map[string]any)["handle"].(string)] = true
		}
		return out
	}
	if got := names(); len(got) != 3 {
		t.Fatalf("before: %v", got)
	}
	restrict(t, ops, alice.account, "ban")
	exec(t, env, `UPDATE max_ids SET status = 'locked' WHERE canonical_name = 'luke'`)
	if got := names(); len(got) != 1 || !got["max.leia"] {
		t.Fatalf("after ban and lock: %v", got)
	}
	exec(t, env, `UPDATE accounts SET status = 'suspended' WHERE id = $1`, carol.account)
	if got := names(); len(got) != 0 {
		t.Fatalf("after suspension: %v", got)
	}
}

func TestMarketGuards_MaxIDCap(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	for _, n := range []string{"bobaa", "bobbb", "bobcc", "bobdd", "bobee", "bobff", "bobgg", "bobhh", "bobii", "bobjj"} {
		claimID(t, bob, n)
	}
	if code, body := buyListing(bob, listingID, "cap-ids-0001"); code != http.StatusForbidden || errCode(body) != "MAX_ID_LIMIT_REACHED" {
		t.Fatalf("buy at the ID cap: %d %v", code, body)
	}
	if code, body := makeOffer(bob, "yoda", "30"); errCode(body) != "MAX_ID_LIMIT_REACHED" {
		t.Fatalf("offer at the ID cap: %d %v", code, body)
	}
}

func TestMarketGuards_CancelLatePaidLapsedOfferRemovesStrike(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	ctx := context.Background()
	alice, bob, ops := newParty(t, env), newParty(t, env), newAdmin(t, env)
	id := claimID(t, alice, "yoda")
	offerID, saleID := seedOfferSale(t, env, id, alice, bob)
	exec(t, env, `UPDATE market_sales SET status = 'expired' WHERE id = $1`, saleID)
	if err := env.market.LapseAcceptedOffer(ctx, offerID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, env, `SELECT count(*) FROM market_strikes WHERE offer_id = $1`, offerID); n != 1 {
		t.Fatalf("strikes after lapse %d", n)
	}
	// Paid late after the name moved on: manual review.
	exec(t, env, `UPDATE market_sales SET status = 'manual_review', tx_hash = $2, submitted_at = now() WHERE id = $1`, saleID, solSigFor(91))
	if code, body := ops.c.do("POST", "/v1/admin/market/sales/"+saleID.String()+"/cancel", map[string]any{"reason": "refunded"}); code != http.StatusOK {
		t.Fatalf("cancel: %d %v", code, body)
	}
	if n := count(t, env, `SELECT count(*) FROM market_strikes WHERE offer_id = $1`, offerID); n != 0 {
		t.Fatalf("strikes after cancel %d", n)
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action = 'market.strike_removed' AND resource_id = $1`, offerID.String()); n != 1 {
		t.Fatalf("audit %d", n)
	}
}
