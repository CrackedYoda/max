//go:build integration

package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/config"
	"github.com/max/max-core/internal/maxids"
)

// newAdmin is a signed-in party on the ADMIN_ACCOUNT_IDS list.
func newAdmin(t *testing.T, env *testEnv) party {
	t.Helper()
	p := newParty(t, env)
	env.admins[p.account] = true
	return p
}

// withReview sends sales priced above 10 USDC to pending_review.
func withReview() envOption {
	return withMarket(func(c *config.MarketConfig) { c.ReviewThreshold = "10" })
}

func adminRoutes() [][2]string {
	id := uuid.NewString()
	return [][2]string{
		{"GET", "/v1/admin/market/listings"},
		{"GET", "/v1/admin/market/offers"},
		{"GET", "/v1/admin/market/sales"},
		{"POST", "/v1/admin/market/listings/" + id + "/delist"},
		{"POST", "/v1/admin/market/sales/" + id + "/approve"},
		{"POST", "/v1/admin/market/sales/" + id + "/deny"},
		{"POST", "/v1/admin/market/sales/" + id + "/cancel"},
		{"POST", "/v1/admin/market/sales/" + id + "/reverse"},
		{"GET", "/v1/admin/market/restrictions"},
		{"POST", "/v1/admin/market/restrictions"},
		{"DELETE", "/v1/admin/market/restrictions/" + id},
	}
}

// Non-admins are refused on every admin market route. The shared admin gate
// (middleware.Admin) answers 404 rather than 403 so the admin surface is not
// advertised; the market routes reuse it.
func TestMarketAdmin_NonAdminRefused(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice := newParty(t, env)
	anon := &client{t: t, h: env.handler}
	for _, r := range adminRoutes() {
		body := map[string]any{"reason": "x", "accountId": alice.account.String(), "kind": "ban"}
		if code, resp := alice.c.do(r[0], r[1], body); code != http.StatusNotFound || errCode(resp) != "NOT_FOUND" {
			t.Errorf("non-admin %s %s: %d %v", r[0], r[1], code, resp)
		}
		if code, _ := anon.do(r[0], r[1], body); code != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s: %d", r[0], r[1], code)
		}
	}
	if n := count(t, env, `SELECT count(*) FROM market_restrictions`); n != 0 {
		t.Fatalf("a non-admin created %d restrictions", n)
	}
}

func TestMarketAdmin_DisabledReturns404(t *testing.T) {
	env := newTestEnv(t)
	ops := newAdmin(t, env)
	for _, r := range adminRoutes() {
		code, body := ops.c.do(r[0], r[1], map[string]any{"reason": "x", "accountId": ops.account.String(), "kind": "ban"})
		if code != http.StatusNotFound || errCode(body) != "MARKET_DISABLED" {
			t.Errorf("%s %s: %d %v", r[0], r[1], code, body)
		}
	}
}

func saleStatus(t *testing.T, env *testEnv, id string) string {
	return str(t, env, `SELECT status FROM market_sales WHERE id = $1`, id)
}

func listingStatus(t *testing.T, env *testEnv, id string) string {
	return str(t, env, `SELECT status FROM market_listings WHERE id = $1`, id)
}

func adminAudit(t *testing.T, env *testEnv, action, resource string, admin party) int {
	return count(t, env, `SELECT count(*) FROM audit_logs WHERE action = $1 AND resource_id = $2 AND actor_account = $3`,
		action, resource, admin.account)
}

func saleEvents(t *testing.T, env *testEnv, saleID, kind string) int {
	return count(t, env, `SELECT count(*) FROM market_sale_events WHERE sale_id = $1 AND event_type = $2`, saleID, kind)
}

func lockIn(t *testing.T, sale map[string]any) time.Duration {
	t.Helper()
	lock, err := time.Parse(time.RFC3339Nano, sale["lockExpiresAt"].(string))
	if err != nil {
		t.Fatalf("lockExpiresAt: %v", sale)
	}
	return time.Until(lock)
}

func TestMarketAdmin_ApproveStartsLock(t *testing.T) {
	env := newTestEnv(t, withReview())
	alice, bob, carol, ops := newParty(t, env), newParty(t, env), newParty(t, env), newAdmin(t, env)

	// Buy now above the threshold waits for review with the listing locked.
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	code, body := buyListing(bob, listingID, "review-buy-0001")
	if code != http.StatusCreated || saleOf(t, body)["status"] != "pending_review" || data(t, body)["instructions"] != nil {
		t.Fatalf("buy: %d %v", code, body)
	}
	saleID := saleOf(t, body)["id"].(string)
	if listingStatus(t, env, listingID) != "locked" {
		t.Fatal("listing must lock while the sale awaits review")
	}

	// The admin view shows the sale in full.
	_, body = ops.c.do("GET", "/v1/admin/market/sales?status=pending_review&name=yoda&accountId="+bob.account.String(), nil)
	rows := body["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("admin sales: %v", body)
	}
	row := rows[0].(map[string]any)
	if row["id"] != saleID || row["buyerAddress"] != bob.wallet.address() || row["sellerPayoutAddress"] != alice.wallet.address() ||
		row["buyerAccountId"] != bob.account.String() || row["sellerAccountId"] != alice.account.String() || row["handle"] != "max.yoda" {
		t.Fatalf("admin sale row: %v", row)
	}

	code, body = ops.c.do("POST", "/v1/admin/market/sales/"+saleID+"/approve", nil)
	if code != http.StatusOK || data(t, body)["status"] != "locked" {
		t.Fatalf("approve: %d %v", code, body)
	}
	if d := lockIn(t, data(t, body)); d < 14*time.Minute || d > 16*time.Minute {
		t.Fatalf("buy-now approval lock in %v, want MARKET_BUY_LOCK", d)
	}
	if adminAudit(t, env, "market.sale_approved", saleID, ops) != 1 || saleEvents(t, env, saleID, "approved") != 1 {
		t.Fatal("approval must write an audit entry with the admin and a sale event")
	}
	if listingStatus(t, env, listingID) != "locked" {
		t.Fatal("listing must stay locked after approval")
	}
	// The buyer now gets a quote and instructions from the sale view.
	code, body = bob.c.do("GET", "/v1/market/sales/"+saleID, nil)
	if code != http.StatusOK || data(t, body)["quote"] == nil || data(t, body)["instructions"] == nil {
		t.Fatalf("buyer sale view after approval: %d %v", code, body)
	}
	if code, body = ops.c.do("POST", "/v1/admin/market/sales/"+saleID+"/approve", nil); code != http.StatusConflict {
		t.Fatalf("second approval: %d %v", code, body)
	}

	// An accepted offer above the threshold: approval uses the pay window.
	jedi := claimID(t, alice, "jedi")
	makeListable(t, env, jedi)
	code, body = makeOffer(carol, "jedi", "30")
	if code != http.StatusCreated {
		t.Fatalf("offer: %d %v", code, body)
	}
	offerID := data(t, body)["id"].(string)
	code, body = alice.c.do("POST", "/v1/market/offers/"+offerID+"/accept", map[string]any{"payoutAddress": alice.wallet.address()})
	if code != http.StatusCreated || saleOf(t, body)["status"] != "pending_review" {
		t.Fatalf("accept: %d %v", code, body)
	}
	offerSale := saleOf(t, body)["id"].(string)
	code, body = ops.c.do("POST", "/v1/admin/market/sales/"+offerSale+"/approve", map[string]any{"reason": "known collector"})
	if code != http.StatusOK {
		t.Fatalf("approve offer sale: %d %v", code, body)
	}
	if d := lockIn(t, data(t, body)); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("offer approval lock in %v, want MARKET_OFFER_PAY_WINDOW", d)
	}
	code, body = carol.c.do("POST", "/v1/market/offers/"+offerID+"/pay", map[string]any{"idempotencyKey": "pay-key-0001"})
	if code != http.StatusOK || data(t, body)["instructions"] == nil {
		t.Fatalf("pay after approval: %d %v", code, body)
	}
}

func TestMarketAdmin_DenyBuyNowAndOffer(t *testing.T) {
	env := newTestEnv(t, withReview())
	alice, bob, carol, ops := newParty(t, env), newParty(t, env), newParty(t, env), newAdmin(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")

	// Buy now: the sale is cancelled and the listing is for sale again.
	_, body := buyListing(bob, listingID, "deny-buy-0001")
	saleID := saleOf(t, body)["id"].(string)
	if code, body := ops.c.do("POST", "/v1/admin/market/sales/"+saleID+"/deny", nil); code != http.StatusBadRequest {
		t.Fatalf("deny without reason: %d %v", code, body)
	}
	code, body := ops.c.do("POST", "/v1/admin/market/sales/"+saleID+"/deny", map[string]any{"reason": "card testing pattern"})
	if code != http.StatusOK || data(t, body)["status"] != "cancelled" {
		t.Fatalf("deny: %d %v", code, body)
	}
	if listingStatus(t, env, listingID) != "active" {
		t.Fatal("deny must release the listing")
	}
	if adminAudit(t, env, "market.sale_denied", saleID, ops) != 1 || saleEvents(t, env, saleID, "denied") != 1 {
		t.Fatal("deny must write audit and event rows")
	}

	// Offer: declined with the admin's reason, no strike; superseded offers stay superseded.
	_, body = makeOffer(bob, "yoda", "30")
	bobOffer := data(t, body)["id"].(string)
	_, body = makeOffer(carol, "yoda", "20")
	carolOffer := data(t, body)["id"].(string)
	code, body = alice.c.do("POST", "/v1/market/offers/"+bobOffer+"/accept", nil)
	if code != http.StatusCreated {
		t.Fatalf("accept: %d %v", code, body)
	}
	offerSale := saleOf(t, body)["id"].(string)
	if code, body = ops.c.do("POST", "/v1/admin/market/sales/"+offerSale+"/deny", map[string]any{"reason": "wash trade"}); code != http.StatusOK {
		t.Fatalf("deny offer sale: %d %v", code, body)
	}
	if offerStatus(t, env, bobOffer) != "declined" || offerStatus(t, env, carolOffer) != "superseded" || listingStatus(t, env, listingID) != "active" {
		t.Fatalf("after offer deny: bob %s carol %s listing %s", offerStatus(t, env, bobOffer), offerStatus(t, env, carolOffer), listingStatus(t, env, listingID))
	}
	if note := str(t, env, `SELECT decline_note FROM market_offers WHERE id = $1`, bobOffer); note != "denied by admin: wash trade" {
		t.Fatalf("decline note %q", note)
	}
	if n := count(t, env, `SELECT count(*) FROM market_strikes WHERE account_id = $1`, bob.account); n != 0 {
		t.Fatalf("deny gave the buyer %d strikes", n)
	}
	// The admin offer view shows the decline.
	_, body = ops.c.do("GET", "/v1/admin/market/offers?status=declined&accountId="+bob.account.String(), nil)
	if rows := body["data"].([]any); len(rows) != 1 || rows[0].(map[string]any)["fromAddress"] != bob.wallet.address() {
		t.Fatalf("admin offers: %v", body)
	}
	// Deny only applies to sales awaiting review.
	if code, body = ops.c.do("POST", "/v1/admin/market/sales/"+offerSale+"/deny", map[string]any{"reason": "again"}); code != http.StatusConflict {
		t.Fatalf("second deny: %d %v", code, body)
	}
}

func TestMarketAdmin_CancelReleasesListingAndOffer(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	ctx := context.Background()
	alice, bob, ops := newParty(t, env), newParty(t, env), newAdmin(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")

	// A locked buy-now sale with no transaction.
	_, body := buyListing(bob, listingID, "cancel-buy-0001")
	saleID := saleOf(t, body)["id"].(string)
	if code, body := ops.c.do("POST", "/v1/admin/market/sales/"+saleID+"/cancel", map[string]any{"reason": " "}); code != http.StatusBadRequest {
		t.Fatalf("cancel without reason: %d %v", code, body)
	}
	code, body := ops.c.do("POST", "/v1/admin/market/sales/"+saleID+"/cancel", map[string]any{"reason": "buyer asked"})
	if code != http.StatusOK || data(t, body)["status"] != "cancelled" || listingStatus(t, env, listingID) != "active" {
		t.Fatalf("cancel locked: %d %v listing %s", code, body, listingStatus(t, env, listingID))
	}
	if adminAudit(t, env, "market.sale_cancelled", saleID, ops) != 1 || saleEvents(t, env, saleID, "cancelled") != 1 {
		t.Fatal("cancel must write audit and event rows")
	}

	// A locked offer sale: the accepted offer expires without a strike.
	_, body = makeOffer(bob, "yoda", "30")
	offerID := data(t, body)["id"].(string)
	_, body = alice.c.do("POST", "/v1/market/offers/"+offerID+"/accept", nil)
	offerSale := saleOf(t, body)["id"].(string)
	if code, body = ops.c.do("POST", "/v1/admin/market/sales/"+offerSale+"/cancel", map[string]any{"reason": "dispute"}); code != http.StatusOK {
		t.Fatalf("cancel offer sale: %d %v", code, body)
	}
	if offerStatus(t, env, offerID) != "expired" || listingStatus(t, env, listingID) != "active" {
		t.Fatalf("after cancel: offer %s listing %s", offerStatus(t, env, offerID), listingStatus(t, env, listingID))
	}
	if n := count(t, env, `SELECT count(*) FROM market_strikes WHERE account_id = $1`, bob.account); n != 0 {
		t.Fatalf("cancel gave the buyer %d strikes", n)
	}

	// manual_review: a paid sale parked for a human; the refund is manual.
	_, body = buyListing(bob, listingID, "cancel-buy-0002")
	mrSale := saleOf(t, body)["id"].(string)
	if code, body = bob.c.do("POST", "/v1/market/sales/"+mrSale+"/submit", map[string]any{"txHash": solSigFor(41)}); code != http.StatusOK {
		t.Fatalf("submit: %d %v", code, body)
	}
	exec(t, env, `UPDATE market_sales SET status = 'manual_review', next_check_at = NULL WHERE id = $1`, mrSale)
	if code, body = ops.c.do("POST", "/v1/admin/market/sales/"+mrSale+"/cancel", map[string]any{"reason": "refunded buyer"}); code != http.StatusOK {
		t.Fatalf("cancel manual_review: %d %v", code, body)
	}
	if listingStatus(t, env, listingID) != "active" {
		t.Fatal("cancel from manual_review must release the listing")
	}
	var refund string
	_ = env.pool.QueryRow(ctx, `SELECT detail->>'refund' FROM market_sale_events WHERE sale_id = $1 AND event_type = 'cancelled'`, mrSale).Scan(&refund)
	if refund != "refund handled off-platform" {
		t.Fatalf("cancel event refund note %q", refund)
	}
	// The name trades again.
	if code, body = buyListing(bob, listingID, "cancel-buy-0003"); code != http.StatusCreated {
		t.Fatalf("buy after cancel: %d %v", code, body)
	}
}

func TestMarketAdmin_CancelRefusedWhileSubmitted(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob, ops := newParty(t, env), newParty(t, env), newAdmin(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	_, body := buyListing(bob, listingID, "submitted-buy-0001")
	saleID := saleOf(t, body)["id"].(string)
	if code, body := bob.c.do("POST", "/v1/market/sales/"+saleID+"/submit", map[string]any{"txHash": solSigFor(42)}); code != http.StatusOK {
		t.Fatalf("submit: %d %v", code, body)
	}
	code, body := ops.c.do("POST", "/v1/admin/market/sales/"+saleID+"/cancel", map[string]any{"reason": "stuck"})
	if code != http.StatusConflict {
		t.Fatalf("cancel submitted: %d %v", code, body)
	}
	if saleStatus(t, env, saleID) != "submitted" || listingStatus(t, env, listingID) != "locked" {
		t.Fatal("a refused cancel changed state")
	}
}

// completeSale lists alice's name, has bob buy and pay, and completes it.
func completeSale(t *testing.T, env *testEnv, alice, bob party, name string, sig byte) (uuid.UUID, string, string) {
	t.Helper()
	id := claimID(t, alice, name)
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	_, body := buyListing(bob, listingID, "complete-buy-"+name)
	saleID := saleOf(t, body)["id"].(string)
	if code, body := bob.c.do("POST", "/v1/market/sales/"+saleID+"/submit", map[string]any{"txHash": solSigFor(sig)}); code != http.StatusOK {
		t.Fatalf("submit: %d %v", code, body)
	}
	if s, err := env.market.ObserveSale(context.Background(), uuid.MustParse(saleID)); err != nil || s.Status != "completed" {
		t.Fatalf("complete: %v %+v", err, s)
	}
	return id, listingID, saleID
}

func owner(t *testing.T, env *testEnv, id uuid.UUID) string {
	return str(t, env, `SELECT owner_account_id::text FROM max_ids WHERE id = $1`, id)
}

func TestMarketAdmin_ReverseRestoresSeller(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob, carol, ops := newParty(t, env), newParty(t, env), newParty(t, env), newAdmin(t, env)
	id, listingID, saleID := completeSale(t, env, alice, bob, "yoda", 51)
	if owner(t, env, id) != bob.account.String() {
		t.Fatal("bob should own the name after the sale")
	}
	if code, body := ops.c.do("POST", "/v1/admin/market/sales/"+uuid.NewString()+"/reverse", map[string]any{"reason": "x"}); code != http.StatusNotFound {
		t.Fatalf("unknown sale: %d %v", code, body)
	}
	code, body := ops.c.do("POST", "/v1/admin/market/sales/"+saleID+"/reverse", map[string]any{"reason": "chargeback fraud"})
	if code != http.StatusOK || data(t, body)["status"] != "reversed" {
		t.Fatalf("reverse: %d %v", code, body)
	}
	if owner(t, env, id) != alice.account.String() {
		t.Fatal("reverse must restore the seller")
	}
	if listingStatus(t, env, listingID) != "sold" {
		t.Fatal("the listing stays sold")
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_ownership_events WHERE max_id = $1 AND sale_id = $2`, id, saleID); n != 2 {
		t.Fatalf("ownership events for the sale = %d, want sale + reversal", n)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations WHERE max_id = $1`, id); n != 0 {
		t.Fatalf("reversal preset %d destinations", n)
	}
	if adminAudit(t, env, "market.sale_reversed", saleID, ops) != 1 || saleEvents(t, env, saleID, "reversed") != 1 {
		t.Fatal("reverse must write audit and event rows")
	}
	if code, body = ops.c.do("POST", "/v1/admin/market/sales/"+saleID+"/reverse", map[string]any{"reason": "again"}); code != http.StatusConflict {
		t.Fatalf("second reverse: %d %v", code, body)
	}

	// The buyer resold (here: transferred) the name: refused.
	id2, _, sale2 := completeSale(t, env, alice, bob, "jedi", 52)
	if err := transfer(t, env, maxids.TransferParams{MaxID: id2, From: bob.account, To: carol.account}); err != nil {
		t.Fatal(err)
	}
	code, body = ops.c.do("POST", "/v1/admin/market/sales/"+sale2+"/reverse", map[string]any{"reason": "fraud"})
	if code != http.StatusConflict || errCode(body) != "OWNER_CHANGED" || body["error"].(map[string]any)["message"] != "buyer no longer owns the name" {
		t.Fatalf("reverse after resale: %d %v", code, body)
	}
	if owner(t, env, id2) != carol.account.String() || saleStatus(t, env, sale2) != "completed" {
		t.Fatal("a refused reversal changed state")
	}

	// The buyer has a rename awaiting payment: refused.
	id3, _, sale3 := completeSale(t, env, alice, bob, "luke", 53)
	makeListable(t, env, id3)
	if code, body := requestRename(t, bob.c, id3.String(), "leia", bob.wallet, "rename-reverse-0001"); code != http.StatusCreated {
		t.Fatalf("rename request: %d %v", code, body)
	}
	code, body = ops.c.do("POST", "/v1/admin/market/sales/"+sale3+"/reverse", map[string]any{"reason": "fraud"})
	if code != http.StatusConflict || errCode(body) != "RENAME_PENDING" {
		t.Fatalf("reverse with pending rename: %d %v", code, body)
	}
	if owner(t, env, id3) != bob.account.String() {
		t.Fatal("a refused reversal moved the name")
	}
}

func TestMarketAdmin_ReverseRequiresReason(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob, ops := newParty(t, env), newParty(t, env), newAdmin(t, env)
	id, _, saleID := completeSale(t, env, alice, bob, "yoda", 54)
	for _, body := range []map[string]any{nil, {}, {"reason": "   "}} {
		if code, resp := ops.c.do("POST", "/v1/admin/market/sales/"+saleID+"/reverse", body); code != http.StatusBadRequest || errCode(resp) != "VALIDATION_FAILED" {
			t.Fatalf("reverse with %v: %d %v", body, code, resp)
		}
	}
	if owner(t, env, id) != bob.account.String() || saleStatus(t, env, saleID) != "completed" {
		t.Fatal("a refused reversal changed state")
	}
}

func TestMarketAdmin_DelistShowsReason(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob, ops := newParty(t, env), newParty(t, env), newAdmin(t, env)
	locked := claimID(t, alice, "yoda")
	makeListable(t, env, locked)
	lockedListing := listName(t, alice, locked, "25")
	_, _ = buyListing(bob, lockedListing, "delist-buy-0001")
	code, body := ops.c.do("POST", "/v1/admin/market/listings/"+lockedListing+"/delist", map[string]any{"reason": "complaint"})
	if code != http.StatusConflict || errCode(body) != "NAME_LOCKED" {
		t.Fatalf("delist while locked: %d %v", code, body)
	}

	id := claimID(t, alice, "jedi")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	if code, body = ops.c.do("POST", "/v1/admin/market/listings/"+listingID+"/delist", nil); code != http.StatusBadRequest {
		t.Fatalf("delist without reason: %d %v", code, body)
	}
	code, body = ops.c.do("POST", "/v1/admin/market/listings/"+listingID+"/delist", map[string]any{"reason": "trademark complaint"})
	if code != http.StatusOK || data(t, body)["status"] != "admin_delisted" || data(t, body)["sellerAccountId"] != alice.account.String() {
		t.Fatalf("delist: %d %v", code, body)
	}
	if adminAudit(t, env, "market.listing_admin_delisted", listingID, ops) != 1 {
		t.Fatal("delist must write an audit entry with the admin")
	}
	// The seller sees why on their own listing; buyers cannot buy it.
	_, body = alice.c.do("GET", "/v1/market/me", nil)
	found := false
	for _, l := range data(t, body)["listings"].([]any) {
		if l := l.(map[string]any); l["id"] == listingID {
			found = l["status"] == "admin_delisted" && l["statusReason"] == "trademark complaint"
		}
	}
	if !found {
		t.Fatalf("seller view of the delisted listing: %v", body)
	}
	if code, body = buyListing(bob, listingID, "delist-buy-0002"); errCode(body) != "LISTING_NOT_ACTIVE" {
		t.Fatalf("buy delisted: %d %v", code, body)
	}
	_, body = ops.c.do("GET", "/v1/admin/market/listings?status=admin_delisted&limit=500", nil)
	if rows := body["data"].([]any); len(rows) != 1 || rows[0].(map[string]any)["payoutAddress"] != alice.wallet.address() {
		t.Fatalf("admin listings: %v", body)
	}
	if code, body = ops.c.do("GET", "/v1/admin/market/listings?limit=abc", nil); code != http.StatusBadRequest {
		t.Fatalf("bad limit: %d %v", code, body)
	}
	if code, body = ops.c.do("GET", "/v1/admin/market/sales?status=nope", nil); code != http.StatusBadRequest {
		t.Fatalf("bad status: %d %v", code, body)
	}
}

func restrict(t *testing.T, ops party, account uuid.UUID, kind string) string {
	t.Helper()
	code, body := ops.c.do("POST", "/v1/admin/market/restrictions", map[string]any{"accountId": account.String(), "kind": kind, "reason": "fraud ring"})
	if code != http.StatusCreated || data(t, body)["active"] != true {
		t.Fatalf("restrict: %d %v", code, body)
	}
	return data(t, body)["id"].(string)
}

func TestMarketAdmin_BanBlocksListOfferBuyAndLiftRestores(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob, ops := newParty(t, env), newParty(t, env), newAdmin(t, env)
	yoda := claimID(t, alice, "yoda")
	makeListable(t, env, yoda)
	listingID := listName(t, alice, yoda, "25")
	jedi := claimID(t, alice, "jedi")
	makeListable(t, env, jedi)

	// Validation.
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	for _, body := range []map[string]any{
		{"accountId": bob.account.String(), "kind": "mute", "reason": "x"},
		{"accountId": bob.account.String(), "kind": "ban", "reason": ""},
		{"accountId": bob.account.String(), "kind": "ban", "reason": "x", "until": past},
		{"accountId": "nope", "kind": "ban", "reason": "x"},
	} {
		if code, resp := ops.c.do("POST", "/v1/admin/market/restrictions", body); code != http.StatusBadRequest {
			t.Errorf("restrict %v: %d %v", body, code, resp)
		}
	}
	if code, body := ops.c.do("POST", "/v1/admin/market/restrictions", map[string]any{"accountId": uuid.NewString(), "kind": "ban", "reason": "x"}); code != http.StatusNotFound {
		t.Fatalf("restrict unknown account: %d %v", code, body)
	}

	// A banned buyer can neither offer nor buy.
	buyerBan := restrict(t, ops, bob.account, "ban")
	if adminAudit(t, env, "market.restriction_created", buyerBan, ops) != 1 {
		t.Fatal("restriction must be audited with the admin")
	}
	code, body := makeOffer(bob, "jedi", "30")
	if code != http.StatusForbidden || errCode(body) != "MARKET_RESTRICTED" {
		t.Fatalf("banned buyer offers: %d %v", code, body)
	}
	if code, body = buyListing(bob, listingID, "ban-buy-0001"); code != http.StatusForbidden || errCode(body) != "MARKET_RESTRICTED" {
		t.Fatalf("banned buyer buys: %d %v", code, body)
	}
	// A banned seller cannot list (selling is refused through listability).
	sellerBan := restrict(t, ops, alice.account, "ban")
	code, body = alice.c.do("POST", "/v1/market/listings", map[string]any{
		"maxId": jedi.String(), "price": "25", "chain": "solana-devnet", "asset": devnetUSDC, "payoutAddress": alice.wallet.address(),
	})
	if errCode(body) != "NOT_LISTABLE" || body["error"].(map[string]any)["details"].(map[string]any)["reason"] != "market_banned" {
		t.Fatalf("banned seller lists: %d %v", code, body)
	}
	// A ban does not close the seller's existing listing.
	if listingStatus(t, env, listingID) != "active" {
		t.Fatal("a ban must not cancel existing listings")
	}
	_, body = ops.c.do("GET", "/v1/admin/market/restrictions?accountId="+bob.account.String(), nil)
	if rows := body["data"].([]any); len(rows) != 1 || rows[0].(map[string]any)["id"] != buyerBan {
		t.Fatalf("admin restrictions: %v", body)
	}

	// Lifting restores everything.
	for _, id := range []string{sellerBan, buyerBan} {
		code, body = ops.c.do("DELETE", "/v1/admin/market/restrictions/"+id, nil)
		if code != http.StatusOK || data(t, body)["liftedAt"] == nil || data(t, body)["active"] != false {
			t.Fatalf("lift: %d %v", code, body)
		}
		if adminAudit(t, env, "market.restriction_lifted", id, ops) != 1 {
			t.Fatal("lift must be audited with the admin")
		}
	}
	if code, body = ops.c.do("DELETE", "/v1/admin/market/restrictions/"+buyerBan, nil); code != http.StatusNotFound {
		t.Fatalf("second lift: %d %v", code, body)
	}
	listName(t, alice, jedi, "25")
	if code, body = makeOffer(bob, "jedi", "30"); code != http.StatusCreated {
		t.Fatalf("offer after lift: %d %v", code, body)
	}
	if code, body = buyListing(bob, listingID, "ban-buy-0002"); code != http.StatusCreated {
		t.Fatalf("buy after lift: %d %v", code, body)
	}

	// An offer suspension blocks offers only.
	suspension := restrict(t, ops, bob.account, "offer_suspension")
	if _, body = makeOffer(bob, "yoda", "30"); errCode(body) != "MARKET_RESTRICTED" {
		t.Fatalf("suspended buyer offers: %v", body)
	}
	if code, _ = ops.c.do("DELETE", "/v1/admin/market/restrictions/"+suspension, map[string]any{"reason": "appeal granted"}); code != http.StatusOK {
		t.Fatalf("lift suspension: %d", code)
	}
}
