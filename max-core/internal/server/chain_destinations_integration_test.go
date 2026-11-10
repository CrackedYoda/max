//go:build integration

package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/maxids"
)

func TestChainOverride_ResolveFallbackTransfer(t *testing.T) {
	env := newTestEnv(t)
	alice, bob := newParty(t, env), newParty(t, env)
	evm := newEVM()
	if code, body := alice.c.prove(evm, "link"); code != http.StatusOK {
		t.Fatalf("link: %d %v", code, body)
	}
	id := claimOwn(t, alice, "alice")
	override := "0x52908400098527886e0f7030069857d2e4169ee7"
	code, body := alice.c.do("PUT", "/v1/ids/"+id+"/chain-destinations/bnb", map[string]any{"address": override, "confirmed": true})
	if code != http.StatusOK {
		t.Fatalf("override: %d %v", code, body)
	}
	anon := &client{t: t, h: env.handler}
	_, body = anon.do("GET", "/v1/resolve/max.alice?chain=bnb", nil)
	if got := strings.ToLower(data(t, body)["address"].(string)); got != override {
		t.Fatalf("bnb resolves to %s", got)
	}
	_, body = anon.do("GET", "/v1/resolve/max.alice?chain=base", nil)
	if got := strings.ToLower(data(t, body)["address"].(string)); got != strings.ToLower(evm.address()) {
		t.Fatalf("base falls back to %s", got)
	}
	if code, _ = alice.c.do("DELETE", "/v1/ids/"+id+"/chain-destinations/bnb", nil); code != http.StatusOK {
		t.Fatalf("remove override: %d", code)
	}
	_, body = anon.do("GET", "/v1/resolve/max.alice?chain=bnb", nil)
	if got := strings.ToLower(data(t, body)["address"].(string)); got != strings.ToLower(evm.address()) {
		t.Fatalf("bnb after removal = %s", got)
	}
	alice.c.do("PUT", "/v1/ids/"+id+"/chain-destinations/avalanche", map[string]any{"address": override, "confirmed": true})
	if err := transfer(t, env, maxids.TransferParams{MaxID: uuid.MustParse(id), From: alice.account, To: bob.account}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations WHERE max_id = $1`, id); n != 0 {
		t.Fatalf("destinations after transfer = %d", n)
	}
}

func TestChainOverride_UnlinkRemovesWalletOverride(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	evm := newEVM()
	alice.c.prove(evm, "link")
	id := claimOwn(t, alice, "alice")
	evmWallet := str(t, env, `SELECT id::text FROM wallets WHERE chain_namespace = 'evm' AND account_id = $1`, alice.account)
	if code, body := alice.c.do("PUT", "/v1/ids/"+id+"/chain-destinations/base", map[string]any{"walletId": evmWallet}); code != http.StatusOK {
		t.Fatalf("wallet override: %d %v", code, body)
	}
	if code, _ := alice.c.do("DELETE", "/v1/wallets/"+evmWallet, nil); code != http.StatusNoContent {
		t.Fatalf("unlink: %d", code)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations WHERE max_id = $1 AND chain_key IS NOT NULL`, id); n != 0 {
		t.Fatalf("overrides after unlink = %d", n)
	}
}

func TestChainOverride_RejectsNonEVM(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	id := claimOwn(t, alice, "alice")
	code, body := alice.c.do("PUT", "/v1/ids/"+id+"/chain-destinations/solana", map[string]any{"address": alice.wallet.address(), "confirmed": true})
	if code != http.StatusBadRequest || errCode(body) != "UNSUPPORTED_CHAIN" {
		t.Fatalf("solana override: %d %v", code, body)
	}
}
