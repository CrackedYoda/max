//go:build integration

package server_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/wallets"
)

// Write-skew guards (security review S07, S08): per-account invariants that a
// row lock on the changed row cannot hold under Read Committed are serialized
// on the account row instead.

func activeWallets(t *testing.T, env *testEnv, acct uuid.UUID) int {
	t.Helper()
	return count(t, env, `SELECT count(*) FROM wallets WHERE account_id = $1 AND status = 'active'`, acct)
}

func claimedIDs(t *testing.T, env *testEnv, acct uuid.UUID) int {
	t.Helper()
	return count(t, env, `SELECT count(*) FROM max_ids WHERE owner_account_id = $1 AND NOT is_default`, acct)
}

// race runs every fn at once and returns their errors in order.
func race(fns ...func() error) []error {
	errs := make([]error, len(fns))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = fn()
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

// holdAccountLock takes the account lock in an open transaction, the way a
// concurrent writer would, and returns its commit.
func holdAccountLock(t *testing.T, env *testEnv, acct uuid.UUID) (generated.DBTX, func()) {
	t.Helper()
	ctx := context.Background()
	tx, err := env.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if err := generated.New(tx).LockAccount(ctx, acct); err != nil {
		t.Fatal(err)
	}
	return tx, func() {
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// mustBlock fails unless done stays open while the account lock is held.
func mustBlock(t *testing.T, done <-chan error, what string) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("%s finished while the account lock was held: %v", what, err)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWalletRemove_ConcurrentKeepsOne(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	repo := wallets.NewRepository(env.pool)
	for i := 0; i < 25; i++ {
		acct := bareAccount(t, env, "human")
		w1 := bareWallet(t, env, acct, "solana", "active", time.Hour)
		w2 := bareWallet(t, env, acct, "evm", "active", time.Hour)
		errs := race(
			func() error { return repo.Remove(ctx, acct, w1) },
			func() error { return repo.Remove(ctx, acct, w2) },
		)
		ok, last := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, wallets.ErrLastWallet):
				last++
			default:
				t.Fatalf("round %d: %v", i, err)
			}
		}
		if ok != 1 || last != 1 {
			t.Fatalf("round %d: %d removed, %d refused as last; want 1 and 1", i, ok, last)
		}
		if n := activeWallets(t, env, acct); n != 1 {
			t.Fatalf("round %d: %d active wallets left, want 1", i, n)
		}
	}
}

// Deterministic form of the race above: Remove waits on the account lock.
func TestWalletRemove_WaitsForAccountLock(t *testing.T) {
	env := newTestEnv(t)
	acct := bareAccount(t, env, "human")
	bareWallet(t, env, acct, "solana", "active", time.Hour)
	w2 := bareWallet(t, env, acct, "evm", "active", time.Hour)

	_, release := holdAccountLock(t, env, acct)
	done := make(chan error, 1)
	go func() { done <- wallets.NewRepository(env.pool).Remove(context.Background(), acct, w2) }()
	mustBlock(t, done, "wallet removal")
	release()
	if err := <-done; err != nil {
		t.Fatalf("remove after release: %v", err)
	}
}

// claimUpTo gives acct n claimed IDs.
func claimUpTo(t *testing.T, svc maxids.Service, acct uuid.UUID, tag string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := svc.Claim(context.Background(), acct, maxids.ClaimInput{Name: fmt.Sprintf("%sheld%d", tag, i)}); err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
	}
}

func TestClaim_ConcurrentDistinctNamesAtQuota(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	svc := maxids.NewService(maxids.NewRepository(env.pool), nil)
	acct := bareAccount(t, env, "human")
	claimUpTo(t, svc, acct, "quota", maxids.MaxIDsPerAccount-1)

	const racers = 8
	fns := make([]func() error, racers)
	for i := range fns {
		name := fmt.Sprintf("quotarace%d", i)
		fns[i] = func() error {
			_, err := svc.Claim(ctx, acct, maxids.ClaimInput{Name: name})
			return err
		}
	}
	ok := 0
	for _, err := range race(fns...) {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, maxids.ErrIDLimitReached):
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d of %d racing claims succeeded at the boundary, want 1", ok, racers)
	}
	if n := claimedIDs(t, env, acct); n != maxids.MaxIDsPerAccount {
		t.Fatalf("claimed IDs = %d, want %d", n, maxids.MaxIDsPerAccount)
	}
}

// An open purchase takes a quota slot, so a claim cannot fill it as well.
func TestClaim_CountsOpenPurchase(t *testing.T) {
	env := newTestEnv(t)
	svc := maxids.NewService(maxids.NewRepository(env.pool), nil)
	buyer, seller := bareAccount(t, env, "human"), bareAccount(t, env, "human")
	claimUpTo(t, svc, buyer, "buyer", maxids.MaxIDsPerAccount-1)
	m, err := svc.Claim(context.Background(), seller, maxids.ClaimInput{Name: "sellerforsale"})
	if err != nil {
		t.Fatal(err)
	}
	seedSale(t, env, m.ID, seller, buyer, "locked")

	if _, err := svc.Claim(context.Background(), buyer, maxids.ClaimInput{Name: "buyeronemore"}); !errors.Is(err, maxids.ErrIDLimitReached) {
		t.Fatalf("claim with a purchase in progress = %v, want ErrIDLimitReached", err)
	}
}

// A sale opening holds the buyer's quota lock (market lockBuyerQuota); a claim
// waiting on it sees the new sale once it commits and is refused.
func TestClaim_SerializesWithSaleOpening(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	svc := maxids.NewService(maxids.NewRepository(env.pool), nil)
	buyer, seller := bareAccount(t, env, "human"), bareAccount(t, env, "human")
	claimUpTo(t, svc, buyer, "opener", maxids.MaxIDsPerAccount-1)
	m, err := svc.Claim(ctx, seller, maxids.ClaimInput{Name: "sellerlisted"})
	if err != nil {
		t.Fatal(err)
	}

	tx, commit := holdAccountLock(t, env, buyer)
	if err := maxids.LockQuota(ctx, generated.New(tx), buyer); err != nil {
		t.Fatalf("sale opening quota check: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.Claim(ctx, buyer, maxids.ClaimInput{Name: "openerclaim"})
		done <- err
	}()
	mustBlock(t, done, "claim")

	var listing uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO market_listings (max_id, seller_account_id, chain, asset_address, asset_decimals, price_base_units, payout_address, status)
		VALUES ($1, $2, 'solana', 'usdc', 6, '10000000', 'payout', 'locked') RETURNING id`, m.ID, seller).Scan(&listing); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO market_sales (max_id, listing_id, seller_account_id, buyer_account_id, buyer_address, seller_payout_address, treasury_address,
			chain, asset_address, asset_decimals, price_base_units, fee_bps, fee_base_units, seller_base_units, idempotency_key, status)
		VALUES ($1, $2, $3, $4, 'b', 'p', 't', 'solana', 'usdc', 6, '10000000', 1000, '1000000', '9000000', $5, 'pending_review')`,
		m.ID, listing, seller, buyer, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	commit()

	if err := <-done; !errors.Is(err, maxids.ErrIDLimitReached) {
		t.Fatalf("claim after the sale opened = %v, want ErrIDLimitReached", err)
	}
	if n := claimedIDs(t, env, buyer); n != maxids.MaxIDsPerAccount-1 {
		t.Fatalf("buyer claimed IDs = %d, want %d", n, maxids.MaxIDsPerAccount-1)
	}
}
