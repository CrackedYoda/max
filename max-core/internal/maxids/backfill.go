package maxids

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/database"
)

// maxBackfillErrors caps how many per-account errors the aggregate keeps.
const maxBackfillErrors = 10

// BackfillDefaults gives every account without a default MAX ID its default,
// batch accounts per transaction, and returns how many it created. Each
// account's destinations are its oldest active wallet per namespace. Agent
// accounts get agent-type defaults; human and organization accounts get
// human-type ones.
//
// It walks accounts once, by keyset on (created_at, id), so an account that
// fails is logged, counted and passed over instead of blocking the accounts
// after it; the failures come back as one aggregate error alongside the
// count. Accounts that gain a default concurrently are skipped, and accounts
// row-locked by another transaction are passed over until the next run, so it
// is safe alongside logins and agent creation, and to run again.
func BackfillDefaults(ctx context.Context, pool *pgxpool.Pool, namer DefaultNamer, batch int, log *zap.Logger) (int, error) {
	if batch <= 0 {
		return 0, fmt.Errorf("backfill defaults: batch must be positive, got %d", batch)
	}
	c := backfillCursor{}
	created := 0
	for {
		r, err := backfillBatch(ctx, pool, namer, batch, &c, log)
		if err != nil {
			return created, c.result(err)
		}
		created += r
		if c.done {
			return created, c.result(nil)
		}
	}
}

// backfillCursor is the keyset position plus the failures seen so far.
type backfillCursor struct {
	afterCreatedAt time.Time
	afterID        uuid.UUID
	done           bool
	failed         int
	errs           []error
}

func (c *backfillCursor) fail(err error) {
	c.failed++
	if len(c.errs) < maxBackfillErrors {
		c.errs = append(c.errs, err)
	}
}

// result joins a fatal error, if any, with the per-account failures.
func (c *backfillCursor) result(fatal error) error {
	if fatal == nil && c.failed == 0 {
		return nil
	}
	errs := c.errs
	if fatal != nil {
		errs = append([]error{fatal}, errs...)
	}
	if c.failed == 0 {
		return fmt.Errorf("backfill defaults: %w", errors.Join(errs...))
	}
	return fmt.Errorf("backfill defaults: %d accounts failed: %w", c.failed, errors.Join(errs...))
}

// backfillBatch handles one keyset page in one transaction and advances the
// cursor past it. A failing account is rolled back to its savepoint, recorded
// on the cursor and passed over; only a batch-level error is returned.
func backfillBatch(ctx context.Context, pool *pgxpool.Pool, namer DefaultNamer, batch int, c *backfillCursor, log *zap.Logger) (int, error) {
	created := 0
	next := *c
	err := database.InTx(ctx, pool, func(tx pgx.Tx) error {
		created, next = 0, *c
		accounts, err := generated.New(tx).AccountsWithoutDefault(ctx, generated.AccountsWithoutDefaultParams{
			AfterCreatedAt: c.afterCreatedAt, AfterID: c.afterID, Batch: int32(batch),
		})
		if err != nil {
			return err
		}
		next.done = len(accounts) < batch
		for _, a := range accounts {
			next.afterCreatedAt, next.afterID = a.CreatedAt, a.ID
			ok, err := backfillAccount(ctx, tx, namer, a.ID, a.Type)
			switch {
			case err != nil:
				log.Warn("default MAX ID backfill failed for account", zap.String("account_id", a.ID.String()), zap.Error(err))
				next.fail(fmt.Errorf("account %s: %w", a.ID, err))
			case ok:
				created++
			default:
				log.Debug("default MAX ID already exists; skipped", zap.String("account_id", a.ID.String()))
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	*c = next
	return created, nil
}

// backfillAccount creates one account's default under a savepoint, so an
// account that gained a default concurrently is skipped without aborting the
// batch. It reports whether it created one.
func backfillAccount(ctx context.Context, tx pgx.Tx, namer DefaultNamer, owner uuid.UUID, accountType string) (bool, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = sp.Rollback(ctx) }()
	q := generated.New(sp)

	wallets, err := q.ListWalletsByAccount(ctx, owner) // active only, oldest first
	if err != nil {
		return false, err
	}
	var dests []WalletRef
	taken := map[string]bool{}
	for _, w := range wallets {
		if taken[w.ChainNamespace] {
			continue
		}
		taken[w.ChainNamespace] = true
		dests = append(dests, WalletRef{ID: w.ID, Namespace: chains.Namespace(w.ChainNamespace)})
	}

	typ := TypeHuman
	if accountType == string(TypeAgent) {
		typ = TypeAgent
	}
	var first *WalletRef
	if len(dests) > 0 {
		first = &dests[0]
	}
	m, err := CreateDefault(ctx, q, namer, owner, typ, first, DefaultSourceBackfill)
	if errors.Is(err, ErrDefaultExists) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for i := 1; i < len(dests); i++ {
		d := dests[i]
		if err := q.UpsertDestination(ctx, generated.UpsertDestinationParams{
			MaxID: m.ID, ChainNamespace: string(d.Namespace), WalletID: &d.ID,
		}); err != nil {
			return false, err
		}
	}
	return true, sp.Commit(ctx)
}
