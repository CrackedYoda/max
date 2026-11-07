//go:build integration

package server_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/maxids"
)

func defaultIDOf(t *testing.T, env *testEnv, acct uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	var id uuid.UUID
	var name string
	if err := env.pool.QueryRow(context.Background(),
		`SELECT id, canonical_name FROM max_ids WHERE owner_account_id = $1 AND is_default`, acct).Scan(&id, &name); err != nil {
		t.Fatal(err)
	}
	return id, name
}

func TestClaim_RefusesDefaultShapes(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	_, own := defaultIDOf(t, env, alice.account)
	for _, name := range []string{own, "quiet-river-4821"} {
		code, body := alice.c.do("POST", "/v1/ids", map[string]any{"name": name})
		if code != http.StatusBadRequest || errCode(body) != "INVALID_MAX_ID" || !strings.Contains(fmt.Sprint(body), "reserved format") {
			t.Fatalf("claim %q: %d %v", name, code, body)
		}
	}
	// Ordinary names still work.
	claimID(t, alice, "yoda")
}

func TestCap_IgnoresDefault(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	if defaults(t, env, alice.account) != 1 {
		t.Fatal("expected a default")
	}
	for _, n := range []string{"capaa", "capbb", "capcc", "capdd", "capee", "capff", "capgg", "caphh", "capii", "capjj"} {
		claimID(t, alice, n)
	}
	code, body := alice.c.do("POST", "/v1/ids", map[string]any{"name": "capkk"})
	if code != http.StatusForbidden || errCode(body) != "MAX_ID_LIMIT_REACHED" {
		t.Fatalf("11th claim: %d %v", code, body)
	}
}

func TestMarket_DefaultNotListable(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob := newParty(t, env), newParty(t, env)
	id, name := defaultIDOf(t, env, alice.account)
	makeListable(t, env, id) // even with clean timing facts

	code, body := alice.c.do("POST", "/v1/market/listings", map[string]any{
		"maxId": id.String(), "price": "25", "chain": "solana-devnet", "asset": devnetUSDC, "payoutAddress": alice.wallet.address(),
	})
	if code != http.StatusUnprocessableEntity || errCode(body) != "NOT_LISTABLE" {
		t.Fatalf("list default: %d %v", code, body)
	}
	if code, body := makeOffer(bob, name, "30"); errCode(body) != "NOT_LISTABLE" {
		t.Fatalf("offer on default: %d %v", code, body)
	}
}

func TestMarket_BuyerCapIgnoresDefault(t *testing.T) {
	env := newTestEnv(t, withMarket(nil))
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimID(t, alice, "yoda")
	makeListable(t, env, id)
	listingID := listName(t, alice, id, "25")
	// Nine claimed IDs plus the default: one slot is still free.
	for _, n := range []string{"bobaa", "bobbb", "bobcc", "bobdd", "bobee", "bobff", "bobgg", "bobhh", "bobii"} {
		claimID(t, bob, n)
	}
	if code, body := buyListing(bob, listingID, "buyer-cap-0001"); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("buy with nine claimed + default: %d %v", code, body)
	}
}

func TestRename_DefaultRefused(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	id, _ := defaultIDOf(t, env, alice.account)
	code, body := requestRename(t, alice.c, id.String(), "jedi", alice.wallet, "rename-default-0001")
	if code != http.StatusConflict || errCode(body) != "DEFAULT_ID_LOCKED" {
		t.Fatalf("rename default: %d %v", code, body)
	}
	code, body = alice.c.do("GET", "/v1/ids/"+id.String()+"/rename", nil)
	if code != http.StatusOK || data(t, body)["eligible"] != false {
		t.Fatalf("eligibility: %d %v", code, body)
	}
	// Renaming a claimed ID to a default shape is refused.
	other := claimID(t, alice, "yoda")
	code, body = requestRename(t, alice.c, other.String(), "quiet-river-4821", alice.wallet, "rename-shape-0001")
	if code != http.StatusBadRequest || errCode(body) != "INVALID_MAX_ID" {
		t.Fatalf("rename to default shape: %d %v", code, body)
	}
}

func TestTransferOwnership_DefaultRefused(t *testing.T) {
	env := newTestEnv(t)
	alice, bob := newParty(t, env), newParty(t, env)
	id, _ := defaultIDOf(t, env, alice.account)
	err := transfer(t, env, maxids.TransferParams{MaxID: id, From: alice.account, To: bob.account})
	if !errors.Is(err, maxids.ErrDefaultNotTransferable) {
		t.Fatalf("transfer default: %v", err)
	}
	if got := str(t, env, `SELECT owner_account_id::text FROM max_ids WHERE id = $1`, id); got != alice.account.String() {
		t.Fatalf("owner changed to %s", got)
	}
}
