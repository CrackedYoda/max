package maxids

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/database"
)

// BackfillDefaults gives every account without a default MAX ID its default,
// batch accounts per transaction, and returns how many it created. Each
// account's destinations are its oldest active wallet per namespace. Agent
// accounts get agent-type defaults; human and organization accounts get
// human-type ones. Accounts locked by another transaction are skipped, and an
// account that gains a default concurrently is skipped, so the backfill is
// safe to run alongside logins and agent creation, and to run again.
func BackfillDefaults(ctx context.Context, pool *pgxpool.Pool, namer DefaultNamer, batch int, log *zap.Logger) (int, error) {
	if batch <= 0 {
		return 0, fmt.Errorf("backfill defaults: batch must be positive, got %d", batch)
	}
	total := 0
	for {
		n, seen, err := backfillBatch(ctx, pool, namer, batch, log)
		total += n
		if err != nil {
			return total, err
		}
		if seen == 0 {
			return total, nil
		}
	}
}

// backfillBatch handles one batch in one transaction. It returns the defaults
// created and the accounts it looked at; zero accounts means it is done.
func backfillBatch(ctx context.Context, pool *pgxpool.Pool, namer DefaultNamer, batch int, log *zap.Logger) (created, seen int, err error) {
	err = database.InTx(ctx, pool, func(tx pgx.Tx) error {
		created, seen = 0, 0
		accounts, err := generated.New(tx).AccountsWithoutDefault(ctx, int32(batch))
		if err != nil {
			return err
		}
		seen = len(accounts)
		for _, a := range accounts {
			ok, err := backfillAccount(ctx, tx, namer, a.ID, a.Type)
			if err != nil {
				return fmt.Errorf("backfill default for account %s: %w", a.ID, err)
			}
			if ok {
				created++
			} else {
				log.Debug("default MAX ID already exists; skipped", zap.String("account_id", a.ID.String()))
			}
		}
		return nil
	})
	if err != nil {
		return 0, seen, err
	}
	return created, seen, nil
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
	m, err := CreateDefault(ctx, q, namer, owner, typ, first)
	if errors.Is(err, ErrDefaultExists) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for i := 1; i < len(dests); i++ {
		d := dests[i]
		if err := q.UpsertDestination(ctx, generated.UpsertDestinationParams{
			MaxID: m.ID, ChainNamespace: string(d.Namespace), WalletID: d.ID,
		}); err != nil {
			return false, err
		}
	}
	return true, sp.Commit(ctx)
}
