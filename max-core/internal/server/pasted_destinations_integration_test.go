//go:build integration

package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/maxids"
)

const pastedBTC = "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"

// claimOwn claims a chosen name for p and returns its id.
func claimOwn(t *testing.T, p party, name string) string {
	t.Helper()
	code, body := p.c.do("POST", "/v1/ids", map[string]any{"name": name})
	if code != http.StatusCreated {
		t.Fatalf("claim %s: %d %v", name, code, body)
	}
	return data(t, body)["id"].(string)
}

func paste(c *client, id, ns, addr string) (int, map[string]any) {
	return c.do("PUT", "/v1/ids/"+id+"/destinations/"+ns, map[string]any{"address": addr, "confirmed": true})
}

// destinationOf returns the owner-view destination for ns, or nil.
func destinationOf(t *testing.T, body map[string]any, ns string) map[string]any {
	t.Helper()
	for _, d := range data(t, body)["destinations"].([]any) {
		if m := d.(map[string]any); m["namespace"] == ns {
			return m
		}
	}
	return nil
}

func TestPaste_ResolveRemove(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	id := claimOwn(t, alice, "alice")

	code, body := paste(alice.c, id, "bitcoin", pastedBTC)
	d := destinationOf(t, body, "bitcoin")
	if code != http.StatusOK || d == nil || d["source"] != "pasted" || d["verified"] != false || d["walletId"] != nil {
		t.Fatalf("paste: %d %v", code, body)
	}
	bob := &client{t: t, h: env.handler}
	code, body = bob.do("GET", "/v1/resolve/max.alice?chain=bitcoin", nil)
	if code != http.StatusOK || data(t, body)["address"] != pastedBTC {
		t.Fatalf("resolve: %d %v", code, body)
	}
	if code, body = paste(alice.c, id, "bitcoin", pastedBTC[:len(pastedBTC)-1]+"q"); code != http.StatusBadRequest || errCode(body) != "INVALID_ADDRESS" {
		t.Fatalf("bad checksum: %d %v", code, body)
	}
	if code, body = alice.c.do("PUT", "/v1/ids/"+id+"/destinations/bitcoin", map[string]any{"address": pastedBTC}); errCode(body) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("unconfirmed: %d %v", code, body)
	}
	if code, _ = alice.c.do("DELETE", "/v1/ids/"+id+"/destinations/bitcoin", nil); code != http.StatusOK {
		t.Fatalf("remove: %d", code)
	}
	if code, _ = bob.do("GET", "/v1/resolve/max.alice?chain=bitcoin", nil); code == http.StatusOK {
		t.Fatal("resolve after remove should fail")
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action = 'maxid.destination_set' AND resource_id = $1 AND metadata->>'source' = 'pasted'`, id); n != 1 {
		t.Fatalf("paste audits = %d", n)
	}
}

func TestPaste_LinkReplacesAndBack(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	id := claimOwn(t, alice, "alice")
	other := newSol().address()

	_, body := paste(alice.c, id, "solana", other)
	if d := destinationOf(t, body, "solana"); d["address"] != other || d["source"] != "pasted" {
		t.Fatalf("paste over wallet: %v", d)
	}
	code, body := alice.c.do("PUT", "/v1/ids/"+id+"/destinations/solana", map[string]any{"walletId": alice.walletID.String()})
	if d := destinationOf(t, body, "solana"); code != http.StatusOK || d["address"] != alice.wallet.address() || d["source"] != "wallet" {
		t.Fatalf("wallet over paste: %d %v", code, body)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations WHERE address IS NOT NULL`); n != 0 {
		t.Fatalf("pasted columns left behind: %d", n)
	}
}

func TestPaste_UnlinkWalletKeepsPasted(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	evm := newEVM()
	if code, body := alice.c.prove(evm, "link"); code != http.StatusOK {
		t.Fatalf("link: %d %v", code, body)
	}
	id := claimOwn(t, alice, "alice")
	paste(alice.c, id, "tron", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t")
	evmWallet := str(t, env, `SELECT id::text FROM wallets WHERE chain_namespace = 'evm' AND account_id = $1`, alice.account)
	if code, body := alice.c.do("DELETE", "/v1/wallets/"+evmWallet, nil); code != http.StatusOK {
		t.Fatalf("unlink: %d %v", code, body)
	}
	if code, body := (&client{t: t, h: env.handler}).do("GET", "/v1/resolve/max.alice?chain=tron", nil); code != http.StatusOK {
		t.Fatalf("pasted tron gone after unlink: %d %v", code, body)
	}
}

func TestPaste_TransferClears(t *testing.T) {
	env := newTestEnv(t)
	alice, bob := newParty(t, env), newParty(t, env)
	id := claimOwn(t, alice, "alice")
	paste(alice.c, id, "bitcoin", pastedBTC)
	if err := transfer(t, env, maxids.TransferParams{MaxID: uuid.MustParse(id), From: alice.account, To: bob.account}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations WHERE max_id = $1`, id); n != 0 {
		t.Fatalf("destinations after transfer = %d", n)
	}
}

func TestPaste_AgentCanPaste(t *testing.T) {
	env := newTestEnv(t)
	admin := &client{t: t, h: env.handler}
	admin.prove(newSol(), "login")
	_, _, agent := setupAgent(t, env.handler, admin, "botty", map[string]any{"limits": []any{}}, []string{"ids:write"})
	code, body := agent.do("GET", "/v1/ids", nil)
	if code != http.StatusOK {
		t.Fatalf("agent list: %d %v", code, body)
	}
	var id string
	for _, x := range body["data"].([]any) {
		if m := x.(map[string]any); m["handle"] == "max.botty" {
			id = m["id"].(string)
		}
	}
	if code, body = paste(agent, id, "bitcoin", pastedBTC); code != http.StatusOK {
		t.Fatalf("agent paste: %d %v", code, body)
	}
}

func TestPaste_DefaultFillDoesNotOverwrite(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	def, _ := defaultIDOf(t, env, alice.account)
	pasted := "0x52908400098527886e0f7030069857d2e4169ee7"
	if code, body := paste(alice.c, def.String(), "evm", pasted); code != http.StatusOK {
		t.Fatalf("paste on default: %d %v", code, body)
	}
	if code, body := alice.c.prove(newEVM(), "link"); code != http.StatusOK {
		t.Fatalf("link: %d %v", code, body)
	}
	if got := str(t, env, `SELECT address FROM max_id_destinations WHERE max_id = $1 AND chain_namespace = 'evm'`, def); got != pasted {
		t.Fatalf("default evm destination = %q", got)
	}
}

func TestPaste_OthersAddressStillLinkable(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	id := claimOwn(t, alice, "alice")
	bobWallet := newSol()
	paste(alice.c, id, "solana", bobWallet.address())
	bob := &client{t: t, h: env.handler}
	if code, body := bob.prove(bobWallet, "login"); code != http.StatusOK || !data(t, body)["accountCreated"].(bool) {
		t.Fatalf("bob login with an address alice pasted: %d %v", code, body)
	}
}

func TestPaste_CheckRejectsBothAndNeither(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	id := claimOwn(t, alice, "alice")
	ctx := context.Background()
	if _, err := env.pool.Exec(ctx, `INSERT INTO max_id_destinations (max_id, chain_namespace, wallet_id, address, confirmed_at) VALUES ($1, 'tron', $2, 'T', now())`, id, alice.walletID); err == nil {
		t.Fatal("both wallet and address accepted")
	}
	if _, err := env.pool.Exec(ctx, `INSERT INTO max_id_destinations (max_id, chain_namespace) VALUES ($1, 'tron')`, id); err == nil {
		t.Fatal("neither wallet nor address accepted")
	}
	if _, err := env.pool.Exec(ctx, `INSERT INTO max_id_destinations (max_id, chain_namespace, address) VALUES ($1, 'tron', 'T')`, id); err == nil {
		t.Fatal("unconfirmed pasted address accepted")
	}
}
