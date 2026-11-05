//go:build integration

package server_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/db/generated"
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
