package wallets

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/shared"
)

type repository struct {
	pool *pgxpool.Pool
	q    *generated.Queries
}

var _ Repository = (*repository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &repository{pool: pool, q: generated.New(pool)}
}

func (r *repository) ListByAccount(ctx context.Context, accountID uuid.UUID) ([]Wallet, error) {
	rows, err := r.q.ListWalletsByAccount(ctx, accountID)
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Wallet, 0, len(rows))
	for _, w := range rows {
		out = append(out, Wallet{
			ID: w.ID, Namespace: chains.Namespace(w.ChainNamespace), Address: w.Address,
			VerifiedAt: w.VerifiedAt, CreatedAt: w.CreatedAt,
		})
	}
	return out, nil
}

func (r *repository) Remove(ctx context.Context, accountID, walletID uuid.UUID) error {
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		w, err := q.LockActiveWalletForAccount(ctx, generated.LockActiveWalletForAccountParams{ID: walletID, AccountID: accountID})
		if err != nil {
			if database.IsNoRows(err) {
				return ErrWalletNotFound
			}
			return err
		}
		n, err := q.CountActiveWalletsByAccount(ctx, accountID)
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastWallet
		}

		affected, err := q.DeleteDestinationsForWallet(ctx, &walletID)
		if err != nil {
			return err
		}
		if len(affected) > 0 {
			if err := q.BumpResolutionVersions(ctx, affected); err != nil {
				return err
			}
		}
		if err := q.MarkWalletRemoved(ctx, walletID); err != nil {
			return err
		}

		ids := make([]string, 0, len(affected))
		for _, id := range affected {
			ids = append(ids, id.String())
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &accountID, Action: "wallet.removed", ResourceType: "wallet", ResourceID: walletID.String(),
			Metadata: map[string]any{"namespace": w.ChainNamespace, "address": w.Address, "cleared_max_ids": ids},
		})
	})
	if err == nil {
		return nil
	}
	var appErr *shared.AppError
	if errors.As(err, &appErr) {
		return err
	}
	return shared.ErrInternal.WithError(err)
}
