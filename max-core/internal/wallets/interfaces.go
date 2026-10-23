package wallets

import (
	"context"

	"github.com/google/uuid"
)

type Repository interface {
	ListByAccount(ctx context.Context, accountID uuid.UUID) ([]Wallet, error)
	// Remove unlinks the wallet, drops every MAX ID destination pointing at it
	// and bumps those IDs' resolution versions, atomically. It refuses to
	// remove the account's last active wallet.
	Remove(ctx context.Context, accountID, walletID uuid.UUID) error
}

type Service interface {
	List(ctx context.Context, caller uuid.UUID) ([]Wallet, error)
	Remove(ctx context.Context, caller, walletID uuid.UUID) error
}
