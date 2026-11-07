//go:build integration

package server_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
)

type fixedNamer struct {
	names []string
	i     int
}

func (f *fixedNamer) Generate() (string, error) {
	n := f.names[f.i%len(f.names)]
	f.i++
	return n, nil
}
func (f *fixedNamer) Owns(string) bool { return true }

func randomNamer(t *testing.T) maxids.DefaultNamer {
	t.Helper()
	n, err := maxids.NewDefaultNamer(maxids.DefaultStyleRandom)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateDefault_OnePerAccount(t *testing.T) {
	env := newTestEnv(t)
	alice := bareAccount(t, env, "human")
	q := generated.New(env.pool)
	ctx := context.Background()

	m, err := maxids.CreateDefault(ctx, q, randomNamer(t), alice, maxids.TypeHuman, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.IsDefault || !maxids.IsDefaultShape(m.CanonicalName) {
		t.Fatalf("default row: %+v", m)
	}
	var infinite bool
	if err := env.pool.QueryRow(ctx, `SELECT listable_at >= '9999-01-01' FROM max_ids WHERE id = $1`, m.ID).Scan(&infinite); err != nil || !infinite {
		t.Fatalf("listable_at far future: %v %v", infinite, err)
	}
	if n := count(t, env, `SELECT count(*) FROM audit_logs WHERE action = 'maxid.default_created' AND resource_id = $1`, m.ID.String()); n != 1 {
		t.Fatalf("audit entries = %d", n)
	}

	_, err = maxids.CreateDefault(ctx, q, randomNamer(t), alice, maxids.TypeHuman, nil)
	if !errors.Is(err, maxids.ErrDefaultExists) {
		t.Fatalf("second default: %v", err)
	}
	if n := defaults(t, env, alice); n != 1 {
		t.Fatalf("defaults = %d", n)
	}
}

func TestCreateDefault_SetsDestination(t *testing.T) {
	env := newTestEnv(t)
	alice := bareAccount(t, env, "human")
	walletID := bareWallet(t, env, alice, "solana", "active", 0)
	ref := &maxids.WalletRef{ID: walletID, Namespace: chains.NamespaceSolana}
	m, err := maxids.CreateDefault(context.Background(), generated.New(env.pool), randomNamer(t), alice, maxids.TypeHuman, ref)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations WHERE max_id = $1 AND chain_namespace = 'solana' AND wallet_id = $2`, m.ID, walletID); n != 1 {
		t.Fatalf("destinations = %d", n)
	}
}

func TestCreateDefault_RetriesNameCollision(t *testing.T) {
	env := newTestEnv(t)
	alice, bob := bareAccount(t, env, "human"), bareAccount(t, env, "human")
	q := generated.New(env.pool)
	ctx := context.Background()
	if _, err := maxids.CreateDefault(ctx, q, &fixedNamer{names: []string{"aaaaaaaa22222222"}}, alice, maxids.TypeHuman, nil); err != nil {
		t.Fatal(err)
	}
	m, err := maxids.CreateDefault(ctx, q, &fixedNamer{names: []string{"aaaaaaaa22222222", "bbbbbbbb33333333"}}, bob, maxids.TypeHuman, nil)
	if err != nil || m.CanonicalName != "bbbbbbbb33333333" {
		t.Fatalf("retry: %v %+v", err, m)
	}
	// Five collisions in a row give up.
	carol := bareAccount(t, env, "human")
	if _, err := maxids.CreateDefault(ctx, q, &fixedNamer{names: []string{"aaaaaaaa22222222"}}, carol, maxids.TypeHuman, nil); err == nil {
		t.Fatal("expected failure after 5 collisions")
	}
}

func TestCountMaxIDsByOwner_ExcludesDefault(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env) // login gives alice her default
	ctx := context.Background()
	q := generated.New(env.pool)
	if n := defaults(t, env, alice.account); n != 1 {
		t.Fatalf("defaults = %d", n)
	}
	claimID(t, alice, "alice")
	n, err := q.CountMaxIDsByOwner(ctx, alice.account)
	if err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
}

// The down migration keeps default rows (as ordinary IDs). Everything runs in
// a transaction that is rolled back, so the shared test DB stays at version 12.
func TestMigration012_DownKeepsRows(t *testing.T) {
	env := newTestEnv(t)
	alice := bareAccount(t, env, "human")
	ctx := context.Background()
	if _, err := maxids.CreateDefault(ctx, generated.New(env.pool), randomNamer(t), alice, maxids.TypeHuman, nil); err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../db/migrations/000012_default_max_ids.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../db/migrations/000012_default_max_ids.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := env.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows := func() int {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM max_ids WHERE owner_account_id = $1`, alice).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	if rows() != 1 {
		t.Fatal("down migration lost the row")
	}
	var col int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'max_ids' AND column_name = 'is_default'`).Scan(&col)
	if col != 0 {
		t.Fatal("column still present after down")
	}
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	var isDefault bool
	if err := tx.QueryRow(ctx, `SELECT is_default FROM max_ids WHERE owner_account_id = $1`, alice).Scan(&isDefault); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	if isDefault {
		t.Fatal("rows come back as ordinary IDs")
	}
}

// bareAccount inserts an account directly, so it has no default ID.
func bareAccount(t *testing.T, env *testEnv, typ string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := env.pool.QueryRow(context.Background(), `INSERT INTO accounts (type) VALUES ($1) RETURNING id`, typ).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// bareWallet inserts a wallet for acct, created age ago.
func bareWallet(t *testing.T, env *testEnv, acct uuid.UUID, ns, status string, age time.Duration) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := env.pool.QueryRow(context.Background(), `
		INSERT INTO wallets (account_id, chain_namespace, address, proof_message, proof_signature, status, verified_at, created_at)
		VALUES ($1, $2, $3, 'm', 's', $4, now(), now() - $5::interval) RETURNING id`,
		acct, ns, uuid.NewString(), status, age.String()).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func defaults(t *testing.T, env *testEnv, acct uuid.UUID) int {
	t.Helper()
	return count(t, env, `SELECT count(*) FROM max_ids WHERE owner_account_id = $1 AND is_default`, acct)
}

func TestLogin_NewAccountGetsDefault(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	if n := defaults(t, env, alice.account); n != 1 {
		t.Fatalf("defaults = %d", n)
	}
	var typ, name string
	if err := env.pool.QueryRow(context.Background(), `SELECT type, canonical_name FROM max_ids WHERE owner_account_id = $1 AND is_default`, alice.account).Scan(&typ, &name); err != nil {
		t.Fatal(err)
	}
	if typ != "human" || !maxids.IsDefaultShape(name) {
		t.Fatalf("default: type=%s name=%s", typ, name)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations d JOIN max_ids m ON m.id = d.max_id
		WHERE m.owner_account_id = $1 AND m.is_default`, alice.account); n != 1 {
		t.Fatalf("destinations = %d", n)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations d JOIN max_ids m ON m.id = d.max_id
		WHERE m.owner_account_id = $1 AND m.is_default AND d.chain_namespace = 'solana' AND d.wallet_id = $2`, alice.account, alice.walletID); n != 1 {
		t.Fatal("destination is not the login wallet")
	}
	// A returning login does not create another.
	alice.c.prove(alice.wallet, "login")
	if n := defaults(t, env, alice.account); n != 1 {
		t.Fatalf("defaults after second login = %d", n)
	}
}

func TestLogin_ConcurrentNewWallet(t *testing.T) {
	env := newTestEnv(t)
	w := newSol()
	const racers = 2
	nonces := make([]map[string]any, racers)
	clients := make([]*client, racers)
	for i := range nonces {
		clients[i] = &client{t: t, h: env.handler}
		code, ch := clients[i].do("POST", "/v1/wallets/challenge", map[string]any{
			"purpose": "login", "namespace": w.namespace(), "address": w.address(),
		})
		if code != http.StatusCreated {
			t.Fatalf("challenge %d %v", code, ch)
		}
		nonces[i] = data(t, ch)
	}
	codes := make([]int, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i], _ = clients[i].do("POST", "/v1/wallets/verify", map[string]any{
				"nonce": nonces[i]["nonce"], "signature": w.sign(nonces[i]["message"].(string)),
			})
		}(i)
	}
	close(start)
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("login %d: %d", i, c)
		}
	}
	if n := count(t, env, `SELECT count(*) FROM accounts`); n != 1 {
		t.Fatalf("accounts = %d", n)
	}
	if n := count(t, env, `SELECT count(*) FROM max_ids WHERE is_default`); n != 1 {
		t.Fatalf("defaults = %d", n)
	}
}

func TestAgentCreate_GetsDefault(t *testing.T) {
	env := newTestEnv(t)
	admin := newParty(t, env)
	code, body := admin.c.do("POST", "/v1/agents", map[string]any{"name": "Research Bot"})
	if code != http.StatusCreated {
		t.Fatalf("create agent: %d %v", code, body)
	}
	agentID := uuid.MustParse(data(t, body)["id"].(string))
	if n := count(t, env, `SELECT count(*) FROM max_ids WHERE owner_account_id = $1 AND is_default AND type = 'agent'`, agentID); n != 1 {
		t.Fatalf("agent defaults = %d", n)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations d JOIN max_ids m ON m.id = d.max_id WHERE m.owner_account_id = $1`, agentID); n != 0 {
		t.Fatalf("agent default destinations = %d", n)
	}
	// The administrator still has exactly their own.
	if n := defaults(t, env, admin.account); n != 1 {
		t.Fatalf("admin defaults = %d", n)
	}
}

func TestBackfill_Idempotent(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	human := bareAccount(t, env, "human")
	oldest := bareWallet(t, env, human, "solana", "active", 2*time.Hour)
	bareWallet(t, env, human, "solana", "active", time.Hour)
	bareWallet(t, env, human, "evm", "removed", 3*time.Hour)
	agent := bareAccount(t, env, "agent")
	org := bareAccount(t, env, "organization")

	n, err := maxids.BackfillDefaults(ctx, env.pool, randomNamer(t), 2, zap.NewNop())
	if err != nil || n != 3 {
		t.Fatalf("first run: %d %v", n, err)
	}
	n, err = maxids.BackfillDefaults(ctx, env.pool, randomNamer(t), 2, zap.NewNop())
	if err != nil || n != 0 {
		t.Fatalf("second run: %d %v", n, err)
	}
	if c := count(t, env, `SELECT count(*) FROM max_ids WHERE is_default`); c != 3 {
		t.Fatalf("defaults = %d", c)
	}
	for acct, typ := range map[uuid.UUID]string{human: "human", agent: "agent", org: "human"} {
		if c := count(t, env, `SELECT count(*) FROM max_ids WHERE owner_account_id = $1 AND is_default AND type = $2`, acct, typ); c != 1 {
			t.Fatalf("%s default of type %s = %d", acct, typ, c)
		}
	}
	// One destination per namespace: the oldest active wallet; removed wallets are ignored.
	if c := count(t, env, `SELECT count(*) FROM max_id_destinations d JOIN max_ids m ON m.id = d.max_id WHERE m.owner_account_id = $1`, human); c != 1 {
		t.Fatalf("human destinations = %d", c)
	}
	if c := count(t, env, `SELECT count(*) FROM max_id_destinations d JOIN max_ids m ON m.id = d.max_id
		WHERE m.owner_account_id = $1 AND d.chain_namespace = 'solana' AND d.wallet_id = $2`, human, oldest); c != 1 {
		t.Fatal("human destination is not the oldest active solana wallet")
	}
	if c := count(t, env, `SELECT count(*) FROM max_id_destinations d JOIN max_ids m ON m.id = d.max_id WHERE m.owner_account_id = ANY($1)`, []uuid.UUID{agent, org}); c != 0 {
		t.Fatalf("walletless destinations = %d", c)
	}
}

// failFirstNamer fails its first Generate call, then delegates.
type failFirstNamer struct {
	next   maxids.DefaultNamer
	failed bool
}

func (f *failFirstNamer) Generate() (string, error) {
	if !f.failed {
		f.failed = true
		return "", errors.New("injected namer failure")
	}
	return f.next.Generate()
}
func (f *failFirstNamer) Owns(s string) bool { return f.next.Owns(s) }

func TestBackfill_FailingAccountDoesNotBlockOthers(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	at := func(age time.Duration) uuid.UUID {
		var id uuid.UUID
		if err := env.pool.QueryRow(ctx, `INSERT INTO accounts (type, created_at) VALUES ('human', now() - $1::interval) RETURNING id`, age.String()).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first, second, third := at(3*time.Hour), at(2*time.Hour), at(time.Hour)

	// Batch 1: the failing account is its own batch, and the run still reaches the others.
	n, err := maxids.BackfillDefaults(ctx, env.pool, &failFirstNamer{next: randomNamer(t)}, 1, zap.NewNop())
	if n != 2 || err == nil || !strings.Contains(err.Error(), first.String()) || !strings.Contains(err.Error(), "1 accounts failed") {
		t.Fatalf("run with a failing account: %d %v", n, err)
	}
	if defaults(t, env, first) != 0 || defaults(t, env, second) != 1 || defaults(t, env, third) != 1 {
		t.Fatal("wrong accounts got defaults")
	}
	// The next run picks up the account that failed.
	if n, err := maxids.BackfillDefaults(ctx, env.pool, randomNamer(t), 1, zap.NewNop()); n != 1 || err != nil {
		t.Fatalf("rerun: %d %v", n, err)
	}
	if defaults(t, env, first) != 1 {
		t.Fatal("failed account still has no default")
	}
}

// The backfill selects an account whose default another transaction is
// inserting, blocks on the one-default index, and skips it once that commits.
func TestBackfill_RacesCreate(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	acct := bareAccount(t, env, "human")

	tx, err := env.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := maxids.CreateDefault(ctx, generated.New(tx), randomNamer(t), acct, maxids.TypeHuman, nil); err != nil {
		t.Fatal(err)
	}
	type result struct {
		n   int
		err error
	}
	core, logs := observer.New(zap.DebugLevel)
	done := make(chan result, 1)
	go func() {
		n, err := maxids.BackfillDefaults(ctx, env.pool, randomNamer(t), 100, zap.New(core))
		done <- result{n, err}
	}()
	// Wait until the backfill is blocked on our uncommitted default.
	deadline := time.Now().Add(5 * time.Second)
	for count(t, env, `SELECT count(*) FROM pg_locks WHERE NOT granted`) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("backfill never blocked on the concurrent default")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil || r.n != 0 {
		t.Fatalf("backfill: %d %v", r.n, r.err)
	}
	if logs.FilterMessage("default MAX ID already exists; skipped").Len() != 1 {
		t.Fatalf("expected one ErrDefaultExists skip, logs: %v", logs.All())
	}
	if n := defaults(t, env, acct); n != 1 {
		t.Fatalf("defaults = %d", n)
	}
	if n, err := maxids.BackfillDefaults(ctx, env.pool, randomNamer(t), 100, zap.NewNop()); err != nil || n != 0 {
		t.Fatalf("rerun: %d %v", n, err)
	}
}

// NO KEY UPDATE lets foreign-key inserts on a locked account through.
func TestBackfill_LockAllowsForeignKeyInserts(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	acct := bareAccount(t, env, "human")
	tx, err := env.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := generated.New(tx).AccountsWithoutDefault(ctx, generated.AccountsWithoutDefaultParams{Batch: 100})
	if err != nil || len(rows) != 1 {
		t.Fatalf("select: %v %v", rows, err)
	}
	insCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := env.pool.Exec(insCtx, `INSERT INTO wallets (account_id, chain_namespace, address, proof_message, proof_signature, verified_at)
		VALUES ($1, 'solana', $2, 'm', 's', now())`, acct, uuid.NewString()); err != nil {
		t.Fatalf("wallet insert blocked by backfill lock: %v", err)
	}
}
