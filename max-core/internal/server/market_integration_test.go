//go:build integration

package server_test

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/config"
	"github.com/max/max-core/internal/market"
	"github.com/max/max-core/internal/market/settle"
	"github.com/max/max-core/internal/maxids"
)

// seedSale inserts the minimum market rows for a sale on maxID in the given
// status, bypassing the (not yet built) market service.
func seedSale(t *testing.T, env *testEnv, maxID, seller, buyer uuid.UUID, status string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := env.pool.QueryRow(context.Background(), `
		INSERT INTO market_listings (max_id, seller_account_id, chain, asset_address, asset_decimals, price_base_units, payout_address, status)
		VALUES ($1, $2, 'solana', 'usdc', 6, '10000000', 'payout', 'locked') RETURNING id`, maxID, seller).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	var saleID uuid.UUID
	err = env.pool.QueryRow(context.Background(), `
		INSERT INTO market_sales (max_id, listing_id, seller_account_id, buyer_account_id, buyer_address, seller_payout_address, treasury_address,
			chain, asset_address, asset_decimals, price_base_units, fee_bps, fee_base_units, seller_base_units, idempotency_key, status)
		VALUES ($1, $2, $3, $4, 'b', 'p', 't', 'solana', 'usdc', 6, '10000000', 1000, '1000000', '9000000', $5, $6) RETURNING id`,
		maxID, id, seller, buyer, uuid.NewString(), status).Scan(&saleID)
	if err != nil {
		t.Fatal(err)
	}
	return saleID
}

func transfer(t *testing.T, env *testEnv, p maxids.TransferParams) error {
	t.Helper()
	ctx := context.Background()
	return runTx(ctx, env, func(q *generated.Queries) error {
		if _, err := q.LockMaxIDByID(ctx, p.MaxID); err != nil {
			return err
		}
		return maxids.TransferOwnership(ctx, q, p)
	})
}

func runTx(ctx context.Context, env *testEnv, fn func(q *generated.Queries) error) error {
	tx, err := env.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(generated.New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type party struct {
	c        *client
	wallet   *solSigner
	account  uuid.UUID
	walletID uuid.UUID
}

func newParty(t *testing.T, env *testEnv) party {
	t.Helper()
	p := party{c: &client{t: t, h: env.handler}, wallet: newSol()}
	p.c.prove(p.wallet, "login")
	err := env.pool.QueryRow(context.Background(),
		`SELECT account_id, id FROM wallets WHERE address = $1`, p.wallet.address()).Scan(&p.account, &p.walletID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func claimID(t *testing.T, p party, name string) uuid.UUID {
	t.Helper()
	code, body := p.c.do("POST", "/v1/ids", map[string]any{"name": name})
	if code != http.StatusCreated {
		t.Fatalf("claim: %d %v", code, body)
	}
	return uuid.MustParse(data(t, body)["id"].(string))
}

func count(t *testing.T, env *testEnv, query string, args ...any) int {
	t.Helper()
	var n int
	if err := env.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTransferOwnership_ResetsIdentity(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	alice, bob := newParty(t, env), newParty(t, env)
	// Alice also links an EVM wallet so the ID has two destinations.
	if code, body := alice.c.prove(newEVM(), "link"); code != http.StatusOK {
		t.Fatalf("link: %d %v", code, body)
	}
	id := claimID(t, alice, "yoda")
	path := "/v1/ids/" + id.String()
	if code, body := alice.c.do("PATCH", path, map[string]any{"displayName": "Yoda", "description": "hi", "avatarUrl": "https://x.test/a.png"}); code != http.StatusOK {
		t.Fatalf("profile: %d %v", code, body)
	}
	if code, body := alice.c.do("PUT", path+"/settlement", map[string]any{"chain": "solana", "asset": "native"}); code != http.StatusOK {
		t.Fatalf("settlement: %d %v", code, body)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations WHERE max_id = $1`, id); n != 2 {
		t.Fatalf("destinations before = %d", n)
	}
	var versionBefore int64
	_ = env.pool.QueryRow(ctx, `SELECT resolution_version FROM max_ids WHERE id = $1`, id).Scan(&versionBefore)
	historyBefore := count(t, env, `SELECT count(*) FROM max_id_history`)
	saleID := seedSale(t, env, id, alice.account, bob.account, "confirming")

	err := transfer(t, env, maxids.TransferParams{
		MaxID: id, From: alice.account, To: bob.account, SaleID: &saleID,
		DestinationWalletID: bob.walletID, HoldPeriod: 72 * time.Hour, Reason: "sale",
	})
	if err != nil {
		t.Fatal(err)
	}

	var (
		owner                       uuid.UUID
		name, display, desc, avt    string
		version                     int64
		changed, listable, renAvail *time.Time
	)
	err = env.pool.QueryRow(ctx, `SELECT owner_account_id, canonical_name, display_name, description, avatar_url, resolution_version, owner_changed_at, listable_at, rename_available_at
		FROM max_ids WHERE id = $1`, id).Scan(&owner, &name, &display, &desc, &avt, &version, &changed, &listable, &renAvail)
	if err != nil {
		t.Fatal(err)
	}
	if owner != bob.account || name != "yoda" || display != "" || desc != "" || avt != "" {
		t.Fatalf("identity not reset: %v %q %q %q %q", owner, name, display, desc, avt)
	}
	if version != versionBefore+1 {
		t.Fatalf("resolution_version %d, want %d", version, versionBefore+1)
	}
	if changed == nil || time.Since(*changed) > time.Minute {
		t.Fatalf("owner_changed_at = %v", changed)
	}
	if d := time.Until(*listable); d < 71*time.Hour || d > 73*time.Hour {
		t.Fatalf("listable_at in %v, want ~72h", d)
	}
	if renAvail.Before(*listable) {
		t.Fatalf("rename_available_at %v < listable_at %v", renAvail, listable)
	}
	rows, err := env.pool.Query(ctx, `SELECT chain_namespace, wallet_id FROM max_id_destinations WHERE max_id = $1`, id)
	if err != nil {
		t.Fatal(err)
	}
	var nDest int
	for rows.Next() {
		var ns string
		var w uuid.UUID
		_ = rows.Scan(&ns, &w)
		nDest++
		if ns != "solana" || w != bob.walletID {
			t.Fatalf("destination %s %v", ns, w)
		}
	}
	rows.Close()
	if nDest != 1 {
		t.Fatalf("destinations after = %d", nDest)
	}
	if n := count(t, env, `SELECT count(*) FROM settlement_preferences WHERE max_id = $1`, id); n != 0 {
		t.Fatalf("settlement rows = %d", n)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_ownership_events WHERE max_id = $1 AND event_type = 'transferred' AND sale_id = $2 AND previous_owner = $3 AND next_owner = $4`, id, saleID, alice.account, bob.account); n != 1 {
		t.Fatalf("transferred events = %d", n)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_history`); n != historyBefore {
		t.Fatalf("max_id_history changed: %d -> %d", historyBefore, n)
	}

	// Guards: wrong current owner, and a destination wallet that is not the buyer's.
	if err := transfer(t, env, maxids.TransferParams{MaxID: id, From: alice.account, To: alice.account}); err == nil {
		t.Fatal("transfer from a non-owner succeeded")
	}
	if err := transfer(t, env, maxids.TransferParams{MaxID: id, From: bob.account, To: alice.account, DestinationWalletID: bob.walletID}); err == nil {
		t.Fatal("destination wallet owned by someone else was accepted")
	}
	// No destination: all destinations cleared, none inserted.
	if err := transfer(t, env, maxids.TransferParams{MaxID: id, From: bob.account, To: alice.account}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations WHERE max_id = $1`, id); n != 0 {
		t.Fatalf("destinations with no destination wallet = %d", n)
	}
}

func TestTransferOwnership_StalePaymentRejected(t *testing.T) {
	env := newTestEnv(t)
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	sender := &client{t: t, h: env.handler}
	senderWallet := newSol()
	sender.prove(senderWallet, "login")

	_, body := sender.do("GET", "/v1/resolve/max.yoda?chain=solana", nil)
	version := data(t, body)["resolutionVersion"]
	if err := transfer(t, env, maxids.TransferParams{MaxID: id, From: alice.account, To: bob.account, DestinationWalletID: bob.walletID}); err != nil {
		t.Fatal(err)
	}
	_, body = sender.do("POST", "/v1/transfers/prepare", map[string]any{
		"recipient": "max.yoda", "chain": "solana", "amount": "0.1", "senderAddress": senderWallet.address(),
		"idempotencyKey": "stale-after-sale-1", "resolutionVersion": version,
	})
	if errCode(body) != "RESOLUTION_STALE" {
		t.Fatalf("prepare after ownership change: %v", body)
	}
}

func TestResolve_OwnerChangedAt(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	anon := &client{t: t, h: env.handler}

	_, body := anon.do("GET", "/v1/resolve/max.yoda?chain=solana", nil)
	if _, ok := data(t, body)["ownerChangedAt"]; ok {
		t.Fatalf("ownerChangedAt present before any transfer: %v", body)
	}
	if err := transfer(t, env, maxids.TransferParams{MaxID: id, From: alice.account, To: bob.account, DestinationWalletID: bob.walletID}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/resolve/max.yoda?chain=solana", "/v1/profiles/max.yoda"} {
		_, body = anon.do("GET", path, nil)
		v, ok := data(t, body)["ownerChangedAt"].(string)
		if !ok {
			t.Fatalf("%s: ownerChangedAt missing: %v", path, body)
		}
		if at, err := time.Parse(time.RFC3339, v); err != nil || time.Since(at) > time.Minute {
			t.Fatalf("%s: ownerChangedAt %q (%v)", path, v, err)
		}
	}
	if _, err := env.pool.Exec(ctx, `UPDATE max_ids SET owner_changed_at = now() - interval '31 days' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/resolve/max.yoda?chain=solana", "/v1/profiles/max.yoda"} {
		_, body = anon.do("GET", path, nil)
		if _, ok := data(t, body)["ownerChangedAt"]; ok {
			t.Fatalf("%s: ownerChangedAt present after 31 days: %v", path, body)
		}
	}
}

func TestRename_RefusedWhileListed(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	id := claimID(t, alice, "yoda")
	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO market_listings (max_id, seller_account_id, chain, asset_address, asset_decimals, price_base_units, payout_address, status)
		VALUES ($1, $2, 'solana', 'usdc', 6, '10000000', 'payout', 'active')`, id, alice.account); err != nil {
		t.Fatal(err)
	}
	code, body := requestRename(t, alice.c, id.String(), "jedi", alice.wallet, "rename-listed-0001")
	if code != http.StatusConflict || errCode(body) != "NAME_LISTED" {
		t.Fatalf("rename while listed: %d %v", code, body)
	}
}

func TestEdits_RefusedWhileSaleOpen(t *testing.T) {
	env := newTestEnv(t)
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	path := "/v1/ids/" + id.String()
	if code, body := alice.c.do("PUT", path+"/settlement", map[string]any{"chain": "solana", "asset": "native"}); code != http.StatusOK {
		t.Fatalf("settlement: %d %v", code, body)
	}
	seedSale(t, env, id, alice.account, bob.account, "locked")

	calls := []struct {
		method, path string
		body         map[string]any
	}{
		{"PATCH", path, map[string]any{"displayName": "X"}},
		{"PUT", path + "/destinations/solana", map[string]any{"walletId": alice.walletID.String()}},
		{"DELETE", path + "/destinations/solana", nil},
		{"PUT", path + "/settlement", map[string]any{"chain": "solana", "asset": "native"}},
		{"DELETE", path + "/settlement", nil},
	}
	for _, c := range calls {
		code, body := alice.c.do(c.method, c.path, c.body)
		if code != http.StatusConflict || errCode(body) != "NAME_LOCKED" {
			t.Errorf("%s %s while sale open: %d %v", c.method, c.path, code, body)
		}
	}
	// Rename is refused too, even without a listing row.
	if _, err := env.pool.Exec(context.Background(), `UPDATE market_listings SET status = 'cancelled'`); err != nil {
		t.Fatal(err)
	}
	if code, body := requestRename(t, alice.c, id.String(), "jedi", alice.wallet, "rename-locked-0001"); errCode(body) != "NAME_LOCKED" {
		t.Errorf("rename while sale open: %d %v", code, body)
	}
	// Once the sale is closed, edits work again.
	if _, err := env.pool.Exec(context.Background(), `UPDATE market_sales SET status = 'expired'`); err != nil {
		t.Fatal(err)
	}
	if code, body := alice.c.do("PATCH", path, map[string]any{"displayName": "X"}); code != http.StatusOK {
		t.Errorf("edit after sale closed: %d %v", code, body)
	}
}

func TestClaim_SetsListableAt(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	id := claimID(t, alice, "yoda")
	var listable time.Time
	if err := env.pool.QueryRow(context.Background(), `SELECT listable_at FROM max_ids WHERE id = $1`, id).Scan(&listable); err != nil {
		t.Fatal(err)
	}
	if d := time.Until(listable); d < 71*time.Hour || d > 73*time.Hour {
		t.Fatalf("listable_at in %v, want ~72h", d)
	}
}

func TestTransferOwnership_TypeFollowsOwner(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	human, agent := newParty(t, env), newParty(t, env)
	if _, err := env.pool.Exec(ctx, `UPDATE accounts SET type = 'agent' WHERE id = $1`, agent.account); err != nil {
		t.Fatal(err)
	}
	id := claimID(t, human, "yoda")
	typeOf := func() string {
		var s string
		_ = env.pool.QueryRow(ctx, `SELECT type FROM max_ids WHERE id = $1`, id).Scan(&s)
		return s
	}
	if typeOf() != "human" {
		t.Fatalf("initial type %q", typeOf())
	}
	if err := transfer(t, env, maxids.TransferParams{MaxID: id, From: human.account, To: agent.account}); err != nil {
		t.Fatal(err)
	}
	if typeOf() != "agent" {
		t.Fatalf("after human->agent: %q", typeOf())
	}
	if err := transfer(t, env, maxids.TransferParams{MaxID: id, From: agent.account, To: human.account}); err != nil {
		t.Fatal(err)
	}
	if typeOf() != "human" {
		t.Fatalf("after agent->human: %q", typeOf())
	}
}

func TestTransferOwnership_TypedErrors(t *testing.T) {
	env := newTestEnv(t)
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	if err := transfer(t, env, maxids.TransferParams{MaxID: id, From: bob.account, To: alice.account}); !errors.Is(err, maxids.ErrTransferOwnerMismatch) {
		t.Fatalf("owner mismatch: %v", err)
	}
	if err := transfer(t, env, maxids.TransferParams{MaxID: id, From: alice.account, To: alice.account}); !errors.Is(err, maxids.ErrTransferSameAccount) {
		t.Fatalf("same account: %v", err)
	}
	if errors.Is(maxids.ErrTransferOwnerMismatch, maxids.ErrTransferSameAccount) {
		t.Fatal("sentinels must be distinguishable")
	}
}

func TestTransferOwnership_RefusedWithPendingRename(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	if code, body := requestRename(t, alice.c, id.String(), "jedi", alice.wallet, "rename-pending-0001"); code != http.StatusCreated {
		t.Fatalf("rename request: %d %v", code, body)
	}
	err := transfer(t, env, maxids.TransferParams{MaxID: id, From: alice.account, To: bob.account})
	if !errors.Is(err, maxids.ErrRenamePending) {
		t.Fatalf("transfer with pending rename: %v", err)
	}
	var owner uuid.UUID
	_ = env.pool.QueryRow(ctx, `SELECT owner_account_id FROM max_ids WHERE id = $1`, id).Scan(&owner)
	if owner != alice.account {
		t.Fatal("ownership moved despite pending rename")
	}
}

const devnetUSDC = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"

type envConfig struct {
	market config.MarketConfig
}

type envOption func(*envConfig)

// withMarket enables the marketplace on solana-devnet USDC; mutate adjusts it.
func withMarket(mutate func(*config.MarketConfig)) envOption {
	return func(ec *envConfig) {
		ec.market = config.MarketConfig{
			Enabled: true, Assets: "solana-devnet:" + devnetUSDC, FeeBPS: 1000, HoldPeriod: 72 * time.Hour,
			BuyLock: 15 * time.Minute, OfferPayWindow: 24 * time.Hour, MinOffer: "1", TreasurySolana: newSol().address(),
		}
		if mutate != nil {
			mutate(&ec.market)
		}
	}
}

func newTestMarket(t *testing.T, pool *pgxpool.Pool, ec envConfig) (*market.Service, *fakeSettle) {
	t.Helper()
	cfg, err := market.NewConfig(ec.market, map[string]string{"solana-devnet": "https://rpc.test"})
	if err != nil {
		t.Fatal(err)
	}
	fs := &fakeSettle{obs: settle.Observation{Status: settle.StatusConfirmed, Matches: true, Confirmations: 32}}
	adapters := map[chains.Namespace]settle.Adapter{chains.NamespaceSolana: fs}
	return market.NewService(market.NewRepository(pool), adapters, cfg, market.AllowAll{}, zap.NewNop()), fs
}

// fakeSettle stands in for the Solana settlement adapter; its matching logic
// is covered by the settle package tests.
type fakeSettle struct {
	mu       sync.Mutex
	obs      settle.Observation
	fundsErr error
}

func (f *fakeSettle) Quote(context.Context, settle.Order) (settle.Quote, error) {
	return settle.Quote{NetworkFee: big.NewInt(5000), RentPerATA: big.NewInt(2039280)}, nil
}
func (f *fakeSettle) CheckFunds(context.Context, settle.Order, settle.Quote) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fundsErr
}
func (f *fakeSettle) BuildTx(o settle.Order, _ settle.Quote) (settle.Instructions, error) {
	return settle.Instructions{Solana: []settle.SolanaIx{
		{Kind: "transfer_checked", Amount: o.SellerAmount.String(), Decimals: o.Decimals},
		{Kind: "transfer_checked", Amount: o.Fee.String(), Decimals: o.Decimals},
		{Kind: "memo", Memo: "max:sale:" + o.SaleID.String()},
	}}, nil
}
func (f *fakeSettle) Observe(context.Context, settle.Order, string) (settle.Observation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.obs, nil
}
func (f *fakeSettle) Balances(context.Context, chains.Chain, string) (map[string]*big.Int, error) {
	return map[string]*big.Int{}, nil
}

func makeListable(t *testing.T, env *testEnv, id uuid.UUID) {
	t.Helper()
	if _, err := env.pool.Exec(context.Background(),
		`UPDATE max_ids SET listable_at = now() - interval '1 minute', rename_available_at = now() - interval '1 minute' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
}

func listName(t *testing.T, p party, id uuid.UUID, price string) string {
	t.Helper()
	code, body := p.c.do("POST", "/v1/market/listings", map[string]any{
		"maxId": id.String(), "price": price, "chain": "solana-devnet", "asset": devnetUSDC, "payoutAddress": p.wallet.address(),
	})
	if code != http.StatusCreated {
		t.Fatalf("list: %d %v", code, body)
	}
	return data(t, body)["id"].(string)
}

func buyListing(p party, listingID, key string) (int, map[string]any) {
	return p.c.do("POST", "/v1/market/listings/"+listingID+"/buy", map[string]any{
		"fromAddress": p.wallet.address(), "idempotencyKey": key,
	})
}

func saleOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	return data(t, body)["sale"].(map[string]any)
}

func TestMarket_BuyNowHappyPath(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	ctx := context.Background()
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")

	// Inside the holding period the name is not listable.
	code, body := alice.c.do("POST", "/v1/market/listings", map[string]any{
		"maxId": id.String(), "price": "25", "chain": "solana-devnet", "asset": devnetUSDC, "payoutAddress": alice.wallet.address(),
	})
	if errCode(body) != "NOT_LISTABLE" || body["error"].(map[string]any)["details"].(map[string]any)["reason"] != "holding_period" {
		t.Fatalf("list in hold: %d %v", code, body)
	}
	makeListable(t, env, id)
	_, body = alice.c.do("GET", "/v1/market/eligibility/"+id.String(), nil)
	if !data(t, body)["listable"].(bool) {
		t.Fatalf("eligibility: %v", body)
	}
	listingID := listName(t, alice, id, "25")
	if code, body = bob.c.do("GET", "/v1/market/eligibility/"+id.String(), nil); code != http.StatusNotFound {
		t.Fatalf("eligibility for non-owner: %d %v", code, body)
	}

	// Public read model.
	anon := &client{t: t, h: env.handler}
	_, body = anon.do("GET", "/v1/market/listings?q=yo&sort=price_asc&maxPrice=30", nil)
	if ls := data(t, body)["listings"].([]any); len(ls) != 1 || ls[0].(map[string]any)["price"] != "25" || ls[0].(map[string]any)["handle"] != "max.yoda" {
		t.Fatalf("search: %v", body)
	}
	_, body = anon.do("GET", "/v1/market/listings?minPrice=26", nil)
	if ls := data(t, body)["listings"].([]any); len(ls) != 0 {
		t.Fatalf("search minPrice: %v", body)
	}
	_, body = anon.do("GET", "/v1/market/names/max.yoda", nil)
	if nv := data(t, body); nv["listing"] == nil || nv["listable"] != true {
		t.Fatalf("name view: %v", body)
	}

	// Buy: snapshot, quote and instructions.
	code, body = buyListing(bob, listingID, "happy-buy-0001")
	if code != http.StatusCreated {
		t.Fatalf("buy: %d %v", code, body)
	}
	sale := saleOf(t, body)
	saleID := sale["id"].(string)
	q := data(t, body)["quote"].(map[string]any)
	if sale["status"] != "locked" || sale["fee"] != "2.5" || sale["sellerReceives"] != "22.5" || q["totalAsset"] != "25" || q["networkFee"] != "0.000005" {
		t.Fatalf("buy view: %v", body)
	}
	ins := data(t, body)["instructions"].(map[string]any)["solana"].([]any)
	if memo := ins[len(ins)-1].(map[string]any)["memo"]; memo != "max:sale:"+saleID {
		t.Fatalf("memo %v", memo)
	}
	// The public name view never exposes the payout wallet or internal reasons.
	_, body = anon.do("GET", "/v1/market/names/yoda", nil)
	pub := data(t, body)["listing"].(map[string]any)
	if _, ok := pub["payoutAddress"]; ok || pub["statusReason"] != nil || pub["status"] != "locked" || pub["price"] != "25" {
		t.Fatalf("public listing leaks or is wrong: %v", pub)
	}
	code, body = buyListing(bob, listingID, "happy-buy-0001")
	// Replay with the same key returns the same sale.
	if code != http.StatusOK || saleOf(t, body)["id"] != saleID {
		t.Fatalf("replay: %d %v", code, body)
	}
	// Listing changes are refused while the sale is open.
	if code, body = alice.c.do("PATCH", "/v1/market/listings/"+listingID, map[string]any{"price": "30"}); errCode(body) != "NAME_LOCKED" {
		t.Fatalf("update while locked: %d %v", code, body)
	}
	if code, body = alice.c.do("DELETE", "/v1/market/listings/"+listingID, nil); errCode(body) != "NAME_LOCKED" {
		t.Fatalf("delist while locked: %d %v", code, body)
	}

	// Submit, then the observer completes the sale.
	if code, body = alice.c.do("POST", "/v1/market/sales/"+saleID+"/submit", map[string]any{"txHash": solSigFor(1)}); code != http.StatusNotFound {
		t.Fatalf("seller submit: %d %v", code, body)
	}
	code, body = bob.c.do("POST", "/v1/market/sales/"+saleID+"/submit", map[string]any{"txHash": solSigFor(1)})
	if code != http.StatusOK || saleOf(t, body)["status"] != "submitted" {
		t.Fatalf("submit: %d %v", code, body)
	}
	done, err := env.market.ObserveSale(ctx, uuid.MustParse(saleID))
	if err != nil || done.Status != market.SaleCompleted {
		t.Fatalf("observe: %v %+v", err, done)
	}

	var owner, dest uuid.UUID
	var listingStatus string
	_ = env.pool.QueryRow(ctx, `SELECT owner_account_id FROM max_ids WHERE id = $1`, id).Scan(&owner)
	_ = env.pool.QueryRow(ctx, `SELECT status FROM market_listings WHERE id = $1`, listingID).Scan(&listingStatus)
	_ = env.pool.QueryRow(ctx, `SELECT wallet_id FROM max_id_destinations WHERE max_id = $1 AND chain_namespace = 'solana'`, id).Scan(&dest)
	if owner != bob.account || listingStatus != "sold" || dest != bob.walletID {
		t.Fatalf("after completion: owner %v listing %s dest %v", owner, listingStatus, dest)
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action = 'market.sale_completed' AND resource_id = $1`, saleID); n != 1 {
		t.Fatalf("audit entries = %d", n)
	}
	if n := count(t, env, `SELECT count(*) FROM market_sale_events WHERE sale_id = $1`, saleID); n != 3 {
		t.Fatalf("sale events = %d, want created, submitted, completed", n)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_ownership_events WHERE sale_id = $1`, saleID); n != 1 {
		t.Fatalf("ownership events = %d", n)
	}
	// Completing again is a no-op.
	if again, err := env.market.CompleteObserved(ctx, uuid.MustParse(saleID), 32); err != nil || again.Status != market.SaleCompleted {
		t.Fatalf("complete twice: %v %+v", err, again)
	}

	_, body = anon.do("GET", "/v1/market/names/yoda", nil)
	nv := data(t, body)
	if nv["listing"] != nil || nv["listable"] != false || nv["reason"] != "holding_period" || nv["lastSale"].(map[string]any)["price"] != "25" {
		t.Fatalf("name view after sale: %v", body)
	}
	_, body = bob.c.do("GET", "/v1/market/me", nil)
	if ss := data(t, body)["sales"].([]any); len(ss) != 1 || ss[0].(map[string]any)["status"] != "completed" {
		t.Fatalf("inbox: %v", body)
	}
}

func TestMarket_ConcurrentBuyOneWins(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob, carol := newParty(t, env), newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")

	type result struct {
		code int
		body map[string]any
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, p := range []party{bob, carol} {
		wg.Add(1)
		go func(i int, p party) {
			defer wg.Done()
			<-start
			code, body := buyListing(p, listingID, "race-buy-0001")
			results[i] = result{code, body}
		}(i, p)
	}
	close(start)
	wg.Wait()

	wins, locked := 0, 0
	for _, r := range results {
		switch {
		case r.code == http.StatusCreated:
			wins++
		case errCode(r.body) == "NAME_LOCKED":
			locked++
		default:
			t.Errorf("unexpected: %d %v", r.code, r.body)
		}
	}
	if wins != 1 || locked != 1 {
		t.Fatalf("wins %d locked %d: %v", wins, locked, results)
	}
	if n := count(t, env, `SELECT count(*) FROM market_sales WHERE max_id = $1`, id); n != 1 {
		t.Fatalf("sales = %d", n)
	}
}

func TestMarket_TxHashReuseRejected(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob := newParty(t, env), newParty(t, env)
	var sales []string
	for i, name := range []string{"yoda", "jedi"} {
		id := claimID(t, alice, name)
		makeListable(t, env, id)
		listingID := listName(t, alice, id, "25")
		code, body := buyListing(bob, listingID, "reuse-buy-000"+string(rune('1'+i)))
		if code != http.StatusCreated {
			t.Fatalf("buy %s: %d %v", name, code, body)
		}
		sales = append(sales, saleOf(t, body)["id"].(string))
	}
	sig := solSigFor(7)
	if code, body := bob.c.do("POST", "/v1/market/sales/"+sales[0]+"/submit", map[string]any{"txHash": sig}); code != http.StatusOK {
		t.Fatalf("first submit: %d %v", code, body)
	}
	code, body := bob.c.do("POST", "/v1/market/sales/"+sales[1]+"/submit", map[string]any{"txHash": sig})
	if errCode(body) != "TX_MISMATCH" {
		t.Fatalf("reused hash: %d %v", code, body)
	}
	// A different hash on the first sale is refused too.
	if code, body = bob.c.do("POST", "/v1/market/sales/"+sales[0]+"/submit", map[string]any{"txHash": solSigFor(8)}); errCode(body) != "TX_MISMATCH" {
		t.Fatalf("second hash: %d %v", code, body)
	}
}

func TestMarket_DisabledReturns404(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	anon := &client{t: t, h: env.handler}
	for _, r := range []struct {
		c            *client
		method, path string
	}{
		{anon, "GET", "/v1/market/listings"},
		{anon, "GET", "/v1/market/names/yoda"},
		{alice.c, "POST", "/v1/market/listings"},
		{alice.c, "POST", "/v1/market/listings/" + uuid.NewString() + "/buy"},
		{alice.c, "POST", "/v1/market/sales/" + uuid.NewString() + "/submit"},
		{alice.c, "GET", "/v1/market/me"},
	} {
		code, body := r.c.do(r.method, r.path, map[string]any{})
		if code != http.StatusNotFound || errCode(body) != "MARKET_DISABLED" {
			t.Errorf("%s %s: %d %v", r.method, r.path, code, body)
		}
	}
}

func TestMarket_PausedBlocksTradingNotReadsOrSubmit(t *testing.T) {
	env := newTestEnv(t, withMarket(func(c *config.MarketConfig) { c.TradingPaused = true }))
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	code, body := alice.c.do("POST", "/v1/market/listings", map[string]any{
		"maxId": id.String(), "price": "25", "chain": "solana-devnet", "asset": devnetUSDC, "payoutAddress": alice.wallet.address(),
	})
	if code != http.StatusServiceUnavailable || errCode(body) != "MARKET_PAUSED" {
		t.Fatalf("list while paused: %d %v", code, body)
	}
	if code, body = buyListing(bob, uuid.NewString(), "paused-buy-0001"); errCode(body) != "MARKET_PAUSED" {
		t.Fatalf("buy while paused: %d %v", code, body)
	}
	if code, body = (&client{t: t, h: env.handler}).do("GET", "/v1/market/listings", nil); code != http.StatusOK {
		t.Fatalf("read while paused: %d %v", code, body)
	}
	saleID := seedSale(t, env, id, alice.account, bob.account, "locked")
	if code, body = bob.c.do("POST", "/v1/market/sales/"+saleID.String()+"/submit", map[string]any{"txHash": solSigFor(3)}); code != http.StatusOK {
		t.Fatalf("submit while paused: %d %v", code, body)
	}
}

func TestMarket_SelfTradeViaRemovedWallet(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")

	// Bob once held a wallet that Alice holds now.
	shared := newSol()
	if code, body := bob.c.prove(shared, "link"); code != http.StatusOK {
		t.Fatalf("bob link: %d %v", code, body)
	}
	var wid uuid.UUID
	_ = env.pool.QueryRow(context.Background(), `SELECT id FROM wallets WHERE address = $1`, shared.address()).Scan(&wid)
	if code, body := bob.c.do("DELETE", "/v1/wallets/"+wid.String(), nil); code != http.StatusNoContent && code != http.StatusOK {
		t.Fatalf("bob remove: %d %v", code, body)
	}
	if code, body := alice.c.prove(shared, "link"); code != http.StatusOK {
		t.Fatalf("alice link: %d %v", code, body)
	}
	if code, body := buyListing(bob, listingID, "self-buy-0001"); errCode(body) != "SELF_TRADE" {
		t.Fatalf("buy: %d %v", code, body)
	}
}

func TestMarket_ListingGuards(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	list := func(p party, payout, price string) (int, map[string]any) {
		return p.c.do("POST", "/v1/market/listings", map[string]any{
			"maxId": id.String(), "price": price, "chain": "solana-devnet", "asset": devnetUSDC, "payoutAddress": payout,
		})
	}
	if code, body := list(alice, bob.wallet.address(), "25"); errCode(body) != "VALIDATION_FAILED" {
		t.Fatalf("foreign payout: %d %v", code, body)
	}
	if code, body := list(alice, alice.wallet.address(), "0.5"); errCode(body) != "OFFER_BELOW_MINIMUM" {
		t.Fatalf("below minimum: %d %v", code, body)
	}
	if code, body := list(bob, bob.wallet.address(), "25"); code != http.StatusNotFound {
		t.Fatalf("non-owner: %d %v", code, body)
	}
	listingID := listName(t, alice, id, "25")
	if code, body := list(alice, alice.wallet.address(), "30"); errCode(body) != "NAME_LISTED" {
		t.Fatalf("second listing: %d %v", code, body)
	}
	if code, body := alice.c.do("PATCH", "/v1/market/listings/"+listingID, map[string]any{"price": "30"}); code != http.StatusOK || data(t, body)["price"] != "30" {
		t.Fatalf("update: %d %v", code, body)
	}
	if code, body := bob.c.do("DELETE", "/v1/market/listings/"+listingID, nil); code != http.StatusNotFound {
		t.Fatalf("stranger delist: %d %v", code, body)
	}
	if code, body := alice.c.do("DELETE", "/v1/market/listings/"+listingID, nil); code != http.StatusNoContent {
		t.Fatalf("delist: %d %v", code, body)
	}
	if code, body := buyListing(bob, listingID, "guard-buy-0001"); errCode(body) != "LISTING_NOT_ACTIVE" {
		t.Fatalf("buy cancelled listing: %d %v", code, body)
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action LIKE 'market.listing_%'`); n != 3 {
		t.Fatalf("listing audit entries = %d", n)
	}
}

func TestMarket_CompleteMovesToManualReviewWhenOwnerChanged(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	ctx := context.Background()
	alice, bob, carol := newParty(t, env), newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	_, body := buyListing(bob, listingID, "review-buy-0001")
	saleID := uuid.MustParse(saleOf(t, body)["id"].(string))
	if code, body := bob.c.do("POST", "/v1/market/sales/"+saleID.String()+"/submit", map[string]any{"txHash": solSigFor(4)}); code != http.StatusOK {
		t.Fatalf("submit: %d %v", code, body)
	}
	// The name moves elsewhere (e.g. an admin action) before completion.
	if err := transfer(t, env, maxids.TransferParams{MaxID: id, From: alice.account, To: carol.account}); err != nil {
		t.Fatal(err)
	}
	s, err := env.market.ObserveSale(ctx, saleID)
	if err != nil || s.Status != market.SaleManualReview {
		t.Fatalf("observe: %v %+v", err, s)
	}
	var owner uuid.UUID
	_ = env.pool.QueryRow(ctx, `SELECT owner_account_id FROM max_ids WHERE id = $1`, id).Scan(&owner)
	if owner != carol.account {
		t.Fatalf("owner %v", owner)
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action = 'market.sale_manual_review' AND resource_id = $1`, saleID.String()); n != 1 {
		t.Fatalf("audit = %d", n)
	}
}

func TestMarket_SnapshotImmutable(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	saleID := seedSale(t, env, id, alice.account, bob.account, "locked")

	for _, stmt := range []string{
		`UPDATE market_sales SET price_base_units = '1' WHERE id = $1`,
		`UPDATE market_sales SET buyer_address = 'attacker' WHERE id = $1`,
		`UPDATE market_sales SET seller_payout_address = 'attacker' WHERE id = $1`,
		`UPDATE market_sales SET fee_base_units = '0', seller_base_units = '10000000' WHERE id = $1`,
	} {
		if _, err := env.pool.Exec(ctx, stmt, saleID); err == nil {
			t.Errorf("snapshot change accepted: %s", stmt)
		}
	}
	if _, err := env.pool.Exec(ctx, `UPDATE market_sales SET status = 'submitted', tx_hash = 'abc' WHERE id = $1`, saleID); err != nil {
		t.Fatalf("status/tx_hash update refused: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE market_sales SET tx_hash = 'def' WHERE id = $1`, saleID); err == nil {
		t.Error("tx_hash changed once set")
	}
	// The split CHECK rejects an inconsistent insert.
	_, err := env.pool.Exec(ctx, `
		INSERT INTO market_sales (max_id, offer_id, listing_id, seller_account_id, buyer_account_id, buyer_address, seller_payout_address, treasury_address,
			chain, asset_address, asset_decimals, price_base_units, fee_bps, fee_base_units, seller_base_units, idempotency_key, status)
		SELECT max_id, NULL, listing_id, seller_account_id, buyer_account_id, 'b', 'p', 't', 'solana', 'usdc', 6, '100', 1000, '10', '80', 'split-key', 'expired'
		FROM market_sales WHERE id = $1`, saleID)
	if err == nil {
		t.Error("fee + seller != price accepted")
	}
}

func TestMarket_PublicNameViewHidesModerationAndExpiry(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	ctx := context.Background()
	alice := newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	anon := &client{t: t, h: env.handler}

	if _, err := env.pool.Exec(ctx, `UPDATE market_listings SET expires_at = now() - interval '1 minute' WHERE id = $1`, listingID); err != nil {
		t.Fatal(err)
	}
	_, body := anon.do("GET", "/v1/market/names/yoda", nil)
	if nv := data(t, body); nv["listing"] != nil {
		t.Fatalf("expired listing shown as live: %v", body)
	}

	if _, err := env.pool.Exec(ctx, `INSERT INTO market_restrictions (account_id, kind, reason) VALUES ($1, 'ban', 'fraud')`, alice.account); err != nil {
		t.Fatal(err)
	}
	_, body = anon.do("GET", "/v1/market/names/yoda", nil)
	if nv := data(t, body); nv["listable"] != false || nv["reason"] != "unavailable" {
		t.Fatalf("public ban reason: %v", body)
	}
	_, body = alice.c.do("GET", "/v1/market/eligibility/"+id.String(), nil)
	if e := data(t, body); e["reason"] != "market_banned" {
		t.Fatalf("owner eligibility reason: %v", body)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE accounts SET status = 'suspended' WHERE id = $1`, alice.account); err != nil {
		t.Fatal(err)
	}
	_, body = anon.do("GET", "/v1/market/names/yoda", nil)
	if nv := data(t, body); nv["reason"] != "unavailable" {
		t.Fatalf("public suspension reason: %v", body)
	}
}

func makeOffer(p party, name, amount string) (int, map[string]any) {
	return p.c.do("POST", "/v1/market/offers", map[string]any{
		"name": name, "amount": amount, "chain": "solana-devnet", "asset": devnetUSDC, "fromAddress": p.wallet.address(),
	})
}

func offerStatus(t *testing.T, env *testEnv, id string) string {
	t.Helper()
	var st string
	if err := env.pool.QueryRow(context.Background(), `SELECT status FROM market_offers WHERE id = $1`, id).Scan(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestMarket_OfferAcceptPay(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	ctx := context.Background()
	alice, bob, carol := newParty(t, env), newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "50")

	code, body := makeOffer(bob, "max.yoda", "30")
	if code != http.StatusCreated || data(t, body)["status"] != "live" || data(t, body)["amount"] != "30" {
		t.Fatalf("bob offer: %d %v", code, body)
	}
	bobOffer := data(t, body)["id"].(string)
	if code, body = makeOffer(bob, "yoda", "31"); errCode(body) != "OFFER_LIMIT" {
		t.Fatalf("second offer on the same name: %d %v", code, body)
	}
	if code, body = makeOffer(alice, "yoda", "30"); errCode(body) != "SELF_TRADE" {
		t.Fatalf("self offer: %d %v", code, body)
	}
	if code, body = makeOffer(carol, "yoda", "0.5"); errCode(body) != "OFFER_BELOW_MINIMUM" {
		t.Fatalf("below minimum: %d %v", code, body)
	}
	code, body = makeOffer(carol, "yoda", "25")
	if code != http.StatusCreated {
		t.Fatalf("carol offer: %d %v", code, body)
	}
	carolOffer := data(t, body)["id"].(string)

	// Public view: a live offer count, never amounts or buyers.
	anon := &client{t: t, h: env.handler}
	_, body = anon.do("GET", "/v1/market/names/yoda", nil)
	if nv := data(t, body); nv["liveOfferCount"] != float64(2) || nv["offers"] != nil {
		t.Fatalf("public name view: %v", body)
	}
	// The seller sees live offers without the buyer's wallet; the buyer sees all their own.
	_, body = alice.c.do("GET", "/v1/market/me", nil)
	recv := data(t, body)["offersReceived"].([]any)
	if len(recv) != 2 {
		t.Fatalf("offers received: %v", body)
	}
	if _, ok := recv[0].(map[string]any)["fromAddress"]; ok {
		t.Fatalf("seller inbox leaks the buyer wallet: %v", recv[0])
	}
	if _, err := env.pool.Exec(ctx, `UPDATE market_offers SET status = 'unfunded' WHERE id = $1`, carolOffer); err != nil {
		t.Fatal(err)
	}
	_, body = alice.c.do("GET", "/v1/market/me", nil)
	if recv = data(t, body)["offersReceived"].([]any); len(recv) != 1 || recv[0].(map[string]any)["id"] != bobOffer {
		t.Fatalf("unfunded offer shown to the seller: %v", recv)
	}
	_, body = carol.c.do("GET", "/v1/market/me", nil)
	if made := data(t, body)["offersMade"].([]any); len(made) != 1 || made[0].(map[string]any)["status"] != "unfunded" {
		t.Fatalf("buyer's own offers: %v", body)
	}

	// Only the owner accepts; the buyer cannot pay before acceptance.
	if code, body = carol.c.do("POST", "/v1/market/offers/"+bobOffer+"/accept", nil); code != http.StatusNotFound {
		t.Fatalf("stranger accept: %d %v", code, body)
	}
	if code, body = bob.c.do("POST", "/v1/market/offers/"+bobOffer+"/pay", map[string]any{"idempotencyKey": "pay-key-0001"}); code != http.StatusConflict {
		t.Fatalf("pay before accept: %d %v", code, body)
	}

	// Accept: the payout comes from the listing, which locks.
	code, body = alice.c.do("POST", "/v1/market/offers/"+bobOffer+"/accept", nil)
	if code != http.StatusCreated {
		t.Fatalf("accept: %d %v", code, body)
	}
	sale := saleOf(t, body)
	saleID := sale["id"].(string)
	lock, _ := time.Parse(time.RFC3339Nano, sale["lockExpiresAt"].(string))
	if sale["status"] != "locked" || sale["offerId"] != bobOffer || sale["listingId"] != nil || sale["price"] != "30" ||
		sale["sellerPayoutAddress"] != alice.wallet.address() || sale["buyerAddress"] != bob.wallet.address() || time.Until(lock) < 23*time.Hour {
		t.Fatalf("offer sale: %v", sale)
	}
	if data(t, body)["instructions"] != nil {
		t.Fatalf("seller got payment instructions: %v", body)
	}
	var listingStatus string
	_ = env.pool.QueryRow(ctx, `SELECT status FROM market_listings WHERE id = $1`, listingID).Scan(&listingStatus)
	if listingStatus != "locked" || offerStatus(t, env, bobOffer) != "accepted" || offerStatus(t, env, carolOffer) != "superseded" {
		t.Fatalf("after accept: listing %s bob %s carol %s", listingStatus, offerStatus(t, env, bobOffer), offerStatus(t, env, carolOffer))
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action = 'market.offer_accepted' AND resource_id = $1`, bobOffer); n != 1 {
		t.Fatalf("accept audit = %d", n)
	}
	// An accepted offer with an open sale cannot lapse.
	if err := env.market.LapseAcceptedOffer(ctx, uuid.MustParse(bobOffer)); err == nil {
		t.Fatal("lapsed an offer whose sale is open")
	}

	// Pay returns a fresh quote and the instructions to sign.
	code, body = bob.c.do("POST", "/v1/market/offers/"+bobOffer+"/pay", map[string]any{"idempotencyKey": "pay-key-0001"})
	if code != http.StatusOK || saleOf(t, body)["id"] != saleID || data(t, body)["quote"].(map[string]any)["totalAsset"] != "30" {
		t.Fatalf("pay: %d %v", code, body)
	}
	ins := data(t, body)["instructions"].(map[string]any)["solana"].([]any)
	if memo := ins[len(ins)-1].(map[string]any)["memo"]; memo != "max:sale:"+saleID {
		t.Fatalf("memo %v", memo)
	}
	if code, body = carol.c.do("POST", "/v1/market/offers/"+bobOffer+"/pay", nil); code != http.StatusNotFound {
		t.Fatalf("stranger pay: %d %v", code, body)
	}

	// Submit and observe, as for buy now.
	code, body = bob.c.do("POST", "/v1/market/sales/"+saleID+"/submit", map[string]any{"txHash": solSigFor(7)})
	if code != http.StatusOK || saleOf(t, body)["status"] != "submitted" {
		t.Fatalf("submit: %d %v", code, body)
	}
	done, err := env.market.ObserveSale(ctx, uuid.MustParse(saleID))
	if err != nil || done.Status != market.SaleCompleted {
		t.Fatalf("observe: %v %+v", err, done)
	}
	var owner, dest uuid.UUID
	_ = env.pool.QueryRow(ctx, `SELECT owner_account_id FROM max_ids WHERE id = $1`, id).Scan(&owner)
	_ = env.pool.QueryRow(ctx, `SELECT wallet_id FROM max_id_destinations WHERE max_id = $1 AND chain_namespace = 'solana'`, id).Scan(&dest)
	_ = env.pool.QueryRow(ctx, `SELECT status FROM market_listings WHERE id = $1`, listingID).Scan(&listingStatus)
	if owner != bob.account || dest != bob.walletID {
		t.Fatalf("after completion: owner %v dest %v", owner, dest)
	}
	if offerStatus(t, env, bobOffer) != "completed" || offerStatus(t, env, carolOffer) != "superseded" || listingStatus != "cancelled" {
		t.Fatalf("after completion: bob %s carol %s listing %s", offerStatus(t, env, bobOffer), offerStatus(t, env, carolOffer), listingStatus)
	}
	if n := count(t, env, `SELECT count(*) FROM market_sale_events WHERE sale_id = $1`, saleID); n != 3 {
		t.Fatalf("sale events = %d, want created, submitted, completed", n)
	}
	// The new owner can list: the old listing no longer blocks the name.
	makeListable(t, env, id)
	listName(t, bob, id, "60")
}

func TestMarket_OfferAcceptWithoutListingNeedsPayout(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	_, body := makeOffer(bob, "yoda", "30")
	offerID := data(t, body)["id"].(string)
	if code, body := alice.c.do("POST", "/v1/market/offers/"+offerID+"/accept", nil); errCode(body) != "VALIDATION_FAILED" {
		t.Fatalf("accept without payout: %d %v", code, body)
	}
	if code, body := alice.c.do("POST", "/v1/market/offers/"+offerID+"/accept", map[string]any{"payoutAddress": bob.wallet.address()}); errCode(body) != "VALIDATION_FAILED" {
		t.Fatalf("accept to the buyer's wallet: %d %v", code, body)
	}
	code, body := alice.c.do("POST", "/v1/market/offers/"+offerID+"/accept", map[string]any{"payoutAddress": alice.wallet.address()})
	if code != http.StatusCreated || saleOf(t, body)["sellerPayoutAddress"] != alice.wallet.address() {
		t.Fatalf("accept: %d %v", code, body)
	}
	// Declining or withdrawing an accepted offer is refused.
	if code, body = alice.c.do("POST", "/v1/market/offers/"+offerID+"/decline", map[string]any{"note": "changed my mind"}); code != http.StatusConflict {
		t.Fatalf("decline accepted: %d %v", code, body)
	}
	if code, body = bob.c.do("DELETE", "/v1/market/offers/"+offerID, nil); code != http.StatusConflict {
		t.Fatalf("withdraw accepted: %d %v", code, body)
	}
}

func TestMarket_OfferDeclineAndWithdraw(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob, carol := newParty(t, env), newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	_, body := makeOffer(bob, "yoda", "30")
	bobOffer := data(t, body)["id"].(string)
	_, body = makeOffer(carol, "yoda", "20")
	carolOffer := data(t, body)["id"].(string)
	if code, body := bob.c.do("POST", "/v1/market/offers/"+bobOffer+"/decline", nil); code != http.StatusNotFound {
		t.Fatalf("buyer decline: %d %v", code, body)
	}
	if code, body := alice.c.do("POST", "/v1/market/offers/"+bobOffer+"/decline", map[string]any{"note": "too low"}); code != http.StatusNoContent {
		t.Fatalf("decline: %d %v", code, body)
	}
	if code, body := alice.c.do("DELETE", "/v1/market/offers/"+carolOffer, nil); code != http.StatusNotFound {
		t.Fatalf("seller withdraw: %d %v", code, body)
	}
	if code, body := carol.c.do("DELETE", "/v1/market/offers/"+carolOffer, nil); code != http.StatusNoContent {
		t.Fatalf("withdraw: %d %v", code, body)
	}
	_, body = bob.c.do("GET", "/v1/market/me", nil)
	made := data(t, body)["offersMade"].([]any)[0].(map[string]any)
	if made["status"] != "declined" || made["declineNote"] != "too low" {
		t.Fatalf("declined offer: %v", made)
	}
	if offerStatus(t, env, carolOffer) != "withdrawn" {
		t.Fatalf("carol offer %s", offerStatus(t, env, carolOffer))
	}
	// A closed offer frees the slot for a new one on the same name.
	if code, body := makeOffer(bob, "yoda", "35"); code != http.StatusCreated {
		t.Fatalf("new offer after decline: %d %v", code, body)
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action IN ('market.offer_declined', 'market.offer_withdrawn')`); n != 2 {
		t.Fatalf("audit rows = %d", n)
	}
}

// seedOfferSale inserts an accepted offer and its locked sale directly.
func seedOfferSale(t *testing.T, env *testEnv, maxID uuid.UUID, seller, buyer party) (offerID, saleID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	err := env.pool.QueryRow(ctx, `
		INSERT INTO market_offers (max_id, buyer_account_id, from_address, chain, asset_address, asset_decimals, amount_base_units, status, expires_at)
		VALUES ($1, $2, $3, 'solana-devnet', $4, 6, '30000000', 'accepted', now() + interval '7 days') RETURNING id`,
		maxID, buyer.account, buyer.wallet.address(), devnetUSDC).Scan(&offerID)
	if err != nil {
		t.Fatal(err)
	}
	err = env.pool.QueryRow(ctx, `
		INSERT INTO market_sales (max_id, offer_id, seller_account_id, buyer_account_id, buyer_address, seller_payout_address, treasury_address,
			chain, asset_address, asset_decimals, price_base_units, fee_bps, fee_base_units, seller_base_units, idempotency_key, status, lock_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'solana-devnet', $8, 6, '30000000', 1000, '3000000', '27000000', $9, 'locked', now() + interval '1 day')
		RETURNING id`,
		maxID, offerID, seller.account, buyer.account, buyer.wallet.address(), seller.wallet.address(), newSol().address(), devnetUSDC,
		"offer:"+offerID.String()).Scan(&saleID)
	if err != nil {
		t.Fatal(err)
	}
	return offerID, saleID
}

func TestMarket_OfferPausedBlocksAllButPay(t *testing.T) {
	env := newTestEnv(t, withMarket(func(c *config.MarketConfig) { c.TradingPaused = true }))
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	if code, body := makeOffer(bob, "yoda", "30"); code != http.StatusServiceUnavailable || errCode(body) != "MARKET_PAUSED" {
		t.Fatalf("offer while paused: %d %v", code, body)
	}
	offerID, saleID := seedOfferSale(t, env, id, alice, bob)
	for _, r := range []struct {
		who          party
		method, path string
	}{
		{alice, "POST", "/accept"}, {alice, "POST", "/decline"}, {bob, "DELETE", ""},
	} {
		if code, body := r.who.c.do(r.method, "/v1/market/offers/"+offerID.String()+r.path, nil); errCode(body) != "MARKET_PAUSED" {
			t.Fatalf("%s %s while paused: %d %v", r.method, r.path, code, body)
		}
	}
	code, body := bob.c.do("POST", "/v1/market/offers/"+offerID.String()+"/pay", map[string]any{"idempotencyKey": "pay-key-0001"})
	if code != http.StatusOK || saleOf(t, body)["id"] != saleID.String() || data(t, body)["instructions"] == nil {
		t.Fatalf("pay while paused: %d %v", code, body)
	}
}

func TestMarket_OfferLapseThirdStrikeSuspends(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	ctx := context.Background()
	alice, bob := newParty(t, env), newParty(t, env)
	for i, name := range []string{"yoda", "luke", "leia"} {
		id := claimID(t, alice, name)
		offerID, saleID := seedOfferSale(t, env, id, alice, bob)
		if _, err := env.pool.Exec(ctx, `UPDATE market_sales SET status = 'expired' WHERE id = $1`, saleID); err != nil {
			t.Fatal(err)
		}
		if err := env.market.LapseAcceptedOffer(ctx, offerID); err != nil {
			t.Fatalf("lapse %d: %v", i+1, err)
		}
		if err := env.market.LapseAcceptedOffer(ctx, offerID); err != nil { // idempotent
			t.Fatalf("lapse %d again: %v", i+1, err)
		}
		if offerStatus(t, env, offerID.String()) != "lapsed" {
			t.Fatalf("offer %d not lapsed", i+1)
		}
		n := count(t, env, `SELECT count(*) FROM market_restrictions WHERE account_id = $1 AND kind = 'offer_suspension'`, bob.account)
		if want := map[bool]int{true: 1, false: 0}[i == 2]; n != want {
			t.Fatalf("after strike %d: %d suspensions", i+1, n)
		}
	}
	if n := count(t, env, `SELECT count(*) FROM market_strikes WHERE account_id = $1`, bob.account); n != 3 {
		t.Fatalf("strikes = %d", n)
	}
	var days int
	_ = env.pool.QueryRow(ctx, `SELECT round(extract(epoch FROM until - now()) / 86400)::int FROM market_restrictions WHERE account_id = $1`, bob.account).Scan(&days)
	if days != 30 {
		t.Fatalf("suspended for %d days", days)
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action IN ('market.offer_lapsed', 'market.restriction_created')`); n != 4 {
		t.Fatalf("audit rows = %d", n)
	}
	carol := newParty(t, env)
	id := claimID(t, carol, "han")
	makeListable(t, env, id)
	if code, body := makeOffer(bob, "han", "30"); errCode(body) != "MARKET_RESTRICTED" {
		t.Fatalf("offer while suspended: %d %v", code, body)
	}
}

func TestMarket_OfferFundingRecheck(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	ctx := context.Background()
	alice, bob, carol := newParty(t, env), newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	_, body := makeOffer(bob, "yoda", "30")
	bobOffer := data(t, body)["id"].(string)
	_, body = makeOffer(carol, "yoda", "20")
	carolOffer := data(t, body)["id"].(string)
	stale := func() {
		if _, err := env.pool.Exec(ctx, `UPDATE market_offers SET funded_checked_at = now() - interval '6 minutes'`); err != nil {
			t.Fatal(err)
		}
	}

	// Freshly checked offers are not due.
	if n, err := env.market.RecheckFunding(ctx, 50); err != nil || n != 0 {
		t.Fatalf("fresh recheck: %d %v", n, err)
	}
	env.settle.mu.Lock()
	env.settle.fundsErr = &settle.FundsShortfall{NeedAsset: big.NewInt(1), HaveAsset: big.NewInt(0), NeedNative: big.NewInt(0), HaveNative: big.NewInt(0)}
	env.settle.mu.Unlock()
	stale()
	if n, err := env.market.RecheckFunding(ctx, 50); err != nil || n != 2 {
		t.Fatalf("recheck short: %d %v", n, err)
	}
	if offerStatus(t, env, bobOffer) != "unfunded" || offerStatus(t, env, carolOffer) != "unfunded" {
		t.Fatal("offers not flipped to unfunded")
	}
	// Accepting an unfunded offer fails cleanly.
	if code, body := alice.c.do("POST", "/v1/market/offers/"+bobOffer+"/accept", map[string]any{"payoutAddress": alice.wallet.address()}); errCode(body) != "OFFER_UNFUNDED" {
		t.Fatalf("accept unfunded: %d %v", code, body)
	}

	env.settle.mu.Lock()
	env.settle.fundsErr = nil
	env.settle.mu.Unlock()
	stale()
	if _, err := env.pool.Exec(ctx, `UPDATE market_offers SET expires_at = now() - interval '1 minute' WHERE id = $1`, carolOffer); err != nil {
		t.Fatal(err)
	}
	if n, err := env.market.RecheckFunding(ctx, 50); err != nil || n != 2 {
		t.Fatalf("recheck funded: %d %v", n, err)
	}
	if offerStatus(t, env, bobOffer) != "live" || offerStatus(t, env, carolOffer) != "expired" {
		t.Fatalf("after funding: bob %s carol %s", offerStatus(t, env, bobOffer), offerStatus(t, env, carolOffer))
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action = 'market.offer_funding_changed'`); n != 3 {
		t.Fatalf("funding audit rows = %d", n)
	}

	// Accept re-runs CheckFunds: a shortfall flips the offer and fails.
	env.settle.mu.Lock()
	env.settle.fundsErr = &settle.FundsShortfall{NeedAsset: big.NewInt(1), HaveAsset: big.NewInt(0), NeedNative: big.NewInt(0), HaveNative: big.NewInt(0)}
	env.settle.mu.Unlock()
	if code, body := alice.c.do("POST", "/v1/market/offers/"+bobOffer+"/accept", map[string]any{"payoutAddress": alice.wallet.address()}); errCode(body) != "OFFER_UNFUNDED" {
		t.Fatalf("accept short: %d %v", code, body)
	}
	if offerStatus(t, env, bobOffer) != "unfunded" {
		t.Fatalf("accept did not flip the offer: %s", offerStatus(t, env, bobOffer))
	}
}
