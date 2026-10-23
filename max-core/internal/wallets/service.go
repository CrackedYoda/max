package wallets

import (
	"context"

	"github.com/google/uuid"
)

type service struct {
	repo Repository
}

var _ Service = (*service)(nil)

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) List(ctx context.Context, caller uuid.UUID) ([]Wallet, error) {
	return s.repo.ListByAccount(ctx, caller)
}

func (s *service) Remove(ctx context.Context, caller, walletID uuid.UUID) error {
	return s.repo.Remove(ctx, caller, walletID)
}
