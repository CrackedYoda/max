//go:build integration

package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/market"
)

var allMarketScopes = []string{"market:read", "market:sell", "market:buy"}

// agentBuyer sets up an agent administered by admin with a USDC budget of
// 30 per transfer and 50 per 24h.
func agentBuyer(t *testing.T, env *testEnv, admin party, idName string, scopes []string) (uuid.UUID, *solSigner, *client) {
	t.Helper()
	id, wallet, c := setupAgent(t, env.handler, admin.c, idName, map[string]any{
		"limits":            []map[string]any{{"chain": "solana-devnet", "asset": devnetUSDC, "maxPerTransfer": "30", "maxPer24h": "50"}},
		"allowAnyRecipient": true,
	}, scopes)
	return uuid.MustParse(id), wallet, c
}

func agentBuy(c *client, wallet *solSigner, listingID, key string) (int, map[string]any) {
	return c.do("POST", "/v1/market/listings/"+listingID+"/buy", map[string]any{"fromAddress": wallet.address(), "idempotencyKey": key})
}

func pendingAuthorization(t *testing.T, code int, body map[string]any) string {
	t.Helper()
	d := data(t, body)
	if code != http.StatusAccepted || d["status"] != "pending_approval" || d["authorizationId"] == nil {
		t.Fatalf("want 202 pending_approval: %d %v", code, body)
	}
	return d["authorizationId"].(string)
}

func authorization(t *testing.T, env *testEnv, id string) (decision string, saleID *uuid.UUID, failure *string) {
	t.Helper()
	err := env.pool.QueryRow(context.Background(),
		`SELECT decision, sale_id, failure FROM agent_market_authorizations WHERE id = $1`, id).Scan(&decision, &saleID, &failure)
	if err != nil {
		t.Fatal(err)
	}
	return decision, saleID, failure
}

func TestScope_MissingMarketBuy403(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, dev := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	_, wallet, agent := agentBuyer(t, env, dev, "readbot", []string{"market:read"})

	if code, body := agentBuy(agent, wallet, listingID, "scope-buy-0001"); code != http.StatusForbidden {
		t.Fatalf("buy with market:read only: %d %v", code, body)
	}
	for _, tc := range []struct{ method, path string }{
		{"POST", "/v1/market/listings"}, {"POST", "/v1/market/offers"}, {"POST", "/v1/market/offers/" + uuid.NewString() + "/accept"},
	} {
		if code, _ := agent.do(tc.method, tc.path, map[string]any{}); code != http.StatusForbidden {
			t.Errorf("%s %s with market:read only: %d", tc.method, tc.path, code)
		}
	}
	if code, body := agent.do("GET", "/v1/market/me", nil); code != http.StatusOK {
		t.Fatalf("inbox with market:read: %d %v", code, body)
	}
	// Approving held market actions needs the administrator's session.
	if code, _ := agent.do("POST", "/v1/agent-approvals/market/"+uuid.NewString()+"/approve", nil); code != http.StatusForbidden {
		t.Fatalf("agent key on approval route: %d", code)
	}
}

func TestMarket_AgentBuyWithinLimitsCompletes(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	ctx := context.Background()
	alice, dev := newParty(t, env), newParty(t, env)
	yoda, luke := claimID(t, alice, "yoda"), claimID(t, alice, "luke")
	makeListable(t, env, yoda)
	makeListable(t, env, luke)
	listingID := listName(t, alice, yoda, "25")
	agentID, wallet, agent := agentBuyer(t, env, dev, "buybot", allMarketScopes)

	// The payer must be the agent's own wallet, not its administrator's.
	if code, body := agentBuy(agent, dev.wallet, listingID, "agent-buy-0000"); errCode(body) != "VALIDATION_FAILED" {
		t.Fatalf("administrator's wallet as payer: %d %v", code, body)
	}
	code, body := agentBuy(agent, wallet, listingID, "agent-buy-0001")
	if code != http.StatusCreated {
		t.Fatalf("agent buy: %d %v", code, body)
	}
	sale := saleOf(t, body)
	saleID := sale["id"].(string)
	if n := count(t, env, `SELECT count(*) FROM agent_market_authorizations WHERE agent_account_id = $1 AND decision = 'auto_approved' AND action = 'buy' AND sale_id = $2`, agentID, saleID); n != 1 {
		t.Fatalf("auto-approved authorizations = %d", n)
	}
	if code, body = agent.do("POST", "/v1/market/sales/"+saleID+"/submit", map[string]any{"txHash": solSigFor(7)}); code != http.StatusOK {
		t.Fatalf("agent submit: %d %v", code, body)
	}
	done, err := env.market.ObserveSale(ctx, uuid.MustParse(saleID))
	if err != nil || done.Status != market.SaleCompleted {
		t.Fatalf("observe: %v %+v", err, done)
	}
	var owner uuid.UUID
	_ = env.pool.QueryRow(ctx, `SELECT owner_account_id FROM max_ids WHERE id = $1`, yoda).Scan(&owner)
	if owner != agentID {
		t.Fatalf("owner %v, want the agent %v", owner, agentID)
	}

	// The completed 25 USDC counts toward the 50 USDC 24h budget: a 30 USDC
	// buy (within the per-transfer limit) is now held.
	second := listName(t, alice, luke, "30")
	code, body = agentBuy(agent, wallet, second, "agent-buy-0002")
	pendingAuthorization(t, code, body)
}

func TestMarket_AgentBuyOverLimitApproved(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, dev, carol := newParty(t, env), newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "40")
	agentID, wallet, agent := agentBuyer(t, env, dev, "bigbot", allMarketScopes)

	code, body := agentBuy(agent, wallet, listingID, "agent-buy-0003")
	authID := pendingAuthorization(t, code, body)
	if n := count(t, env, `SELECT count(*) FROM market_sales`); n != 0 {
		t.Fatalf("a held buy opened %d sales", n)
	}
	var status string
	_ = env.pool.QueryRow(context.Background(), `SELECT status FROM market_listings WHERE id = $1`, listingID).Scan(&status)
	if status != market.ListingActive {
		t.Fatalf("listing %s while the buy is held", status)
	}

	// Neither a stranger nor the seller can approve.
	for _, p := range []party{carol, alice} {
		if code, body := p.c.do("POST", "/v1/agent-approvals/market/"+authID+"/approve", nil); code != http.StatusNotFound {
			t.Fatalf("non-administrator approve: %d %v", code, body)
		}
		if code, body := p.c.do("POST", "/v1/agent-approvals/market/"+authID+"/deny", nil); code != http.StatusNotFound {
			t.Fatalf("non-administrator deny: %d %v", code, body)
		}
	}
	if d, _, _ := authorization(t, env, authID); d != "pending_approval" {
		t.Fatalf("decision %s after stranger attempts", d)
	}

	code, body = dev.c.do("POST", "/v1/agent-approvals/market/"+authID+"/approve", nil)
	if code != http.StatusOK {
		t.Fatalf("approve: %d %v", code, body)
	}
	res := data(t, body)
	sale := res["sale"].(map[string]any)["sale"].(map[string]any)
	if res["authorization"].(map[string]any)["decision"] != "approved" || sale["status"] != "locked" || sale["price"] != "40" {
		t.Fatalf("approve result: %v", res)
	}
	saleID := sale["id"].(string)
	var buyer uuid.UUID
	_ = env.pool.QueryRow(context.Background(), `SELECT buyer_account_id FROM market_sales WHERE id = $1`, saleID).Scan(&buyer)
	if d, linked, _ := authorization(t, env, authID); d != "approved" || buyer != agentID || linked == nil || linked.String() != saleID {
		t.Fatalf("after approval: decision %s buyer %v sale %v", d, buyer, linked)
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action = 'agent.market_approved' AND resource_id = $1`, authID); n != 1 {
		t.Fatalf("approval audit entries = %d", n)
	}
	// The agent retrying its key gets the approved sale; approving twice is refused.
	if code, body = agentBuy(agent, wallet, listingID, "agent-buy-0003"); code != http.StatusOK || saleOf(t, body)["id"] != saleID {
		t.Fatalf("agent retry: %d %v", code, body)
	}
	if code, _ = dev.c.do("POST", "/v1/agent-approvals/market/"+authID+"/approve", nil); code != http.StatusConflict {
		t.Fatalf("double approve: %d", code)
	}
}

func TestMarket_AgentApprovalReplayFailureRecorded(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, dev, bob := newParty(t, env), newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "40")
	_, wallet, agent := agentBuyer(t, env, dev, "slowbot", allMarketScopes)
	code, body := agentBuy(agent, wallet, listingID, "agent-buy-0004")
	authID := pendingAuthorization(t, code, body)

	// Someone else buys the name meanwhile.
	if code, body = buyListing(bob, listingID, "bob-buy-00001"); code != http.StatusCreated {
		t.Fatalf("bob buy: %d %v", code, body)
	}
	if code, body = dev.c.do("POST", "/v1/agent-approvals/market/"+authID+"/approve", nil); errCode(body) != "NAME_LOCKED" {
		t.Fatalf("replay on a locked name: %d %v", code, body)
	}
	if d, _, failure := authorization(t, env, authID); d != "approved" || failure == nil || *failure == "" {
		t.Fatalf("failure not recorded: %s %v", d, failure)
	}
}

func TestMarket_AgentListWithoutCanSellDenied(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	dev := newParty(t, env)
	agentID, wallet, agent := agentBuyer(t, env, dev, "sellbot", allMarketScopes)
	var nameID uuid.UUID
	if err := env.pool.QueryRow(context.Background(), `SELECT id FROM max_ids WHERE canonical_name = 'sellbot'`).Scan(&nameID); err != nil {
		t.Fatal(err)
	}
	makeListable(t, env, nameID)

	code, body := agent.do("POST", "/v1/market/listings", map[string]any{
		"maxId": nameID.String(), "price": "25", "chain": "solana-devnet", "asset": devnetUSDC, "payoutAddress": wallet.address(),
	})
	authID := pendingAuthorization(t, code, body)
	if n := count(t, env, `SELECT count(*) FROM market_listings`); n != 0 {
		t.Fatalf("a held listing was created")
	}
	if code, body = dev.c.do("POST", "/v1/agent-approvals/market/"+authID+"/deny", nil); code != http.StatusOK ||
		data(t, body)["authorization"].(map[string]any)["decision"] != "denied" {
		t.Fatalf("deny: %d %v", code, body)
	}
	if n := count(t, env, `SELECT count(*) FROM market_listings`); n != 0 {
		t.Fatalf("a denied listing was created")
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action = 'agent.market_denied' AND resource_id = $1`, authID); n != 1 {
		t.Fatalf("deny audit entries = %d", n)
	}
	if code, _ = dev.c.do("POST", "/v1/agent-approvals/market/"+authID+"/approve", nil); code != http.StatusConflict {
		t.Fatalf("approve after deny: %d", code)
	}

	// With canSell and a price at the minimum, the same listing goes straight through.
	if code, body = dev.c.do("PUT", "/v1/agents/"+agentID.String()+"/policy", map[string]any{
		"allowAnyRecipient": true, "market": map[string]any{"canSell": true, "minSalePrices": []map[string]any{
			{"chain": "solana-devnet", "asset": devnetUSDC, "amount": "25000000"},
		}},
	}); code != http.StatusOK || data(t, body)["market"].(map[string]any)["canSell"] != true {
		t.Fatalf("policy: %d %v", code, body)
	}
	if code, body = agent.do("POST", "/v1/market/listings", map[string]any{
		"maxId": nameID.String(), "price": "25", "chain": "solana-devnet", "asset": devnetUSDC, "payoutAddress": wallet.address(),
	}); code != http.StatusCreated {
		t.Fatalf("list with canSell: %d %v", code, body)
	}
	// Below the minimum sale price a change of price is held.
	listingID := data(t, body)["id"].(string)
	code, body = agent.do("PATCH", "/v1/market/listings/"+listingID, map[string]any{"price": "24"})
	pendingAuthorization(t, code, body)
	// A policy update that leaves market out keeps the selling policy.
	dev.c.do("PUT", "/v1/agents/"+agentID.String()+"/policy", map[string]any{"allowAnyRecipient": true})
	if _, body = dev.c.do("GET", "/v1/agents/"+agentID.String(), nil); data(t, body)["policy"].(map[string]any)["market"].(map[string]any)["canSell"] != true {
		t.Fatalf("market policy reset: %v", body)
	}
}

func TestMarket_AgentApprovalRefusesRepricedListing(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, dev := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "40")
	_, wallet, agent := agentBuyer(t, env, dev, "pricebot", allMarketScopes)
	code, body := agentBuy(agent, wallet, listingID, "agent-buy-0005")
	authID := pendingAuthorization(t, code, body)

	// The seller reprices before the administrator approves the 40 USDC buy.
	if code, body = alice.c.do("PATCH", "/v1/market/listings/"+listingID, map[string]any{"price": "45"}); code != http.StatusOK {
		t.Fatalf("reprice: %d %v", code, body)
	}
	code, body = dev.c.do("POST", "/v1/agent-approvals/market/"+authID+"/approve", nil)
	if errCode(body) != "LISTING_NOT_ACTIVE" {
		t.Fatalf("replay at a new price: %d %v", code, body)
	}
	if n := count(t, env, `SELECT count(*) FROM market_sales`); n != 0 {
		t.Fatalf("%d sales opened at a price nobody approved", n)
	}
	if _, _, failure := authorization(t, env, authID); failure == nil || *failure == "" {
		t.Fatal("failure not recorded")
	}
}

func TestMarket_AdministratorListsHeldActions(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, dev, carol := newParty(t, env), newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "40")
	agentID, wallet, agent := agentBuyer(t, env, dev, "heldbot", allMarketScopes)
	code, body := agentBuy(agent, wallet, listingID, "agent-buy-0006")
	authID := pendingAuthorization(t, code, body)

	// Carol's own agent has a held buy too; it must never show up in dev's list.
	carolAgentID, carolWallet, carolAgent := agentBuyer(t, env, carol, "otherbot", allMarketScopes)
	code, body = agentBuy(carolAgent, carolWallet, listingID, "agent-buy-0007")
	otherID := pendingAuthorization(t, code, body)

	path := "/v1/agents/" + agentID.String() + "/market-authorizations?decision=pending_approval"
	code, body = dev.c.do("GET", path, nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d %v", code, body)
	}
	rows := body["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows %v", rows)
	}
	row := rows[0].(map[string]any)
	sum := row["summary"].(map[string]any)
	if row["id"] != authID || row["action"] != "buy" || row["decision"] != "pending_approval" || row["amountBaseUnits"] != "40000000" ||
		row["chain"] != "solana-devnet" || row["asset"] != devnetUSDC || len(row["reasons"].([]any)) == 0 || row["createdAt"] == nil ||
		sum["op"] != "buy" || sum["name"] != "yoda" || sum["listingId"] != listingID || sum["price"] != "40000000" {
		t.Fatalf("row %v", row)
	}
	for _, r := range rows {
		if r.(map[string]any)["id"] == otherID {
			t.Fatal("another administrator's agent row leaked")
		}
	}

	// Other people, the agent itself and bad filters are refused.
	if code, _ = carol.c.do("GET", path, nil); code != http.StatusNotFound {
		t.Fatalf("other administrator: %d", code)
	}
	if code, _ = dev.c.do("GET", "/v1/agents/"+carolAgentID.String()+"/market-authorizations", nil); code != http.StatusNotFound {
		t.Fatalf("dev reads carol's agent: %d", code)
	}
	if code, _ = agent.do("GET", path, nil); code != http.StatusForbidden {
		t.Fatalf("agent key: %d", code)
	}
	if code, body = dev.c.do("GET", "/v1/agents/"+agentID.String()+"/market-authorizations?decision=bogus", nil); errCode(body) != "VALIDATION_FAILED" {
		t.Fatalf("bad decision filter: %d %v", code, body)
	}

	// Once denied, it leaves the pending list but stays in the full one.
	dev.c.do("POST", "/v1/agent-approvals/market/"+authID+"/deny", nil)
	if _, body = dev.c.do("GET", path, nil); len(body["data"].([]any)) != 0 {
		t.Fatalf("pending after deny: %v", body)
	}
	if _, body = dev.c.do("GET", "/v1/agents/"+agentID.String()+"/market-authorizations", nil); len(body["data"].([]any)) != 1 {
		t.Fatalf("all after deny: %v", body)
	}
}
