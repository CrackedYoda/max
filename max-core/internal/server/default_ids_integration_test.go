//go:build integration

package server_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

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
	alice := newParty(t, env)
	q := generated.New(env.pool)
	ctx := context.Background()

	m, err := maxids.CreateDefault(ctx, q, randomNamer(t), alice.account, maxids.TypeHuman, nil)
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

	_, err = maxids.CreateDefault(ctx, q, randomNamer(t), alice.account, maxids.TypeHuman, nil)
	if !errors.Is(err, maxids.ErrDefaultExists) {
		t.Fatalf("second default: %v", err)
	}
	if n := count(t, env, `SELECT count(*) FROM max_ids WHERE owner_account_id = $1 AND is_default`, alice.account); n != 1 {
		t.Fatalf("defaults = %d", n)
	}
}

func TestCreateDefault_SetsDestination(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	ref := &maxids.WalletRef{ID: alice.walletID, Namespace: chains.NamespaceSolana}
	m, err := maxids.CreateDefault(context.Background(), generated.New(env.pool), randomNamer(t), alice.account, maxids.TypeHuman, ref)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, env, `SELECT count(*) FROM max_id_destinations WHERE max_id = $1 AND chain_namespace = 'solana' AND wallet_id = $2`, m.ID, alice.walletID); n != 1 {
		t.Fatalf("destinations = %d", n)
	}
}

func TestCreateDefault_RetriesNameCollision(t *testing.T) {
	env := newTestEnv(t)
	alice, bob := newParty(t, env), newParty(t, env)
	q := generated.New(env.pool)
	ctx := context.Background()
	if _, err := maxids.CreateDefault(ctx, q, &fixedNamer{names: []string{"aaaaaaaa22222222"}}, alice.account, maxids.TypeHuman, nil); err != nil {
		t.Fatal(err)
	}
	m, err := maxids.CreateDefault(ctx, q, &fixedNamer{names: []string{"aaaaaaaa22222222", "bbbbbbbb33333333"}}, bob.account, maxids.TypeHuman, nil)
	if err != nil || m.CanonicalName != "bbbbbbbb33333333" {
		t.Fatalf("retry: %v %+v", err, m)
	}
	// Five collisions in a row give up.
	carol := newParty(t, env)
	if _, err := maxids.CreateDefault(ctx, q, &fixedNamer{names: []string{"aaaaaaaa22222222"}}, carol.account, maxids.TypeHuman, nil); err == nil {
		t.Fatal("expected failure after 5 collisions")
	}
}

func TestCountMaxIDsByOwner_ExcludesDefault(t *testing.T) {
	env := newTestEnv(t)
	alice := newParty(t, env)
	ctx := context.Background()
	q := generated.New(env.pool)
	if _, err := maxids.CreateDefault(ctx, q, randomNamer(t), alice.account, maxids.TypeHuman, nil); err != nil {
		t.Fatal(err)
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
	alice := newParty(t, env)
	ctx := context.Background()
	if _, err := maxids.CreateDefault(ctx, generated.New(env.pool), randomNamer(t), alice.account, maxids.TypeHuman, nil); err != nil {
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
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM max_ids WHERE owner_account_id = $1`, alice.account).Scan(&n); err != nil {
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
	if err := tx.QueryRow(ctx, `SELECT is_default FROM max_ids WHERE owner_account_id = $1`, alice.account).Scan(&isDefault); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	if isDefault {
		t.Fatal("rows come back as ordinary IDs")
	}
}
