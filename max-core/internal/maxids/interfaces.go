package maxids

import (
	"context"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
)

type Repository interface {
	// GetReserved returns nil when the name has no active reservation.
	GetReserved(ctx context.Context, canonical string) (*Reserved, error)
	// NameStatus returns "" when free, else taken | pending_rename | quarantined.
	NameStatus(ctx context.Context, canonical string) (string, error)
	CountByOwner(ctx context.Context, owner uuid.UUID) (int64, error)
	// Create claims the name and points it at the owner's first verified wallet
	// in each namespace, atomically. Returns ErrNameTaken on a uniqueness race.
	Create(ctx context.Context, owner uuid.UUID, canonical string, typ Type, displayName string) (MaxID, error)
	// ListActiveWallets returns the owner's active linked wallets.
	ListActiveWallets(ctx context.Context, owner uuid.UUID) ([]WalletRef, error)
	// RecordGateCheck audits a registration balance gate evaluation. counted
	// is nil when the gate passed without totalling every asset.
	RecordGateCheck(ctx context.Context, owner uuid.UUID, requiredUSD float64, countedUSD *float64, passed bool) error
	ListByOwner(ctx context.Context, owner uuid.UUID) ([]MaxID, error)
	GetForOwner(ctx context.Context, id, owner uuid.UUID) (MaxID, error)
	UpdateProfile(ctx context.Context, id, owner uuid.UUID, in ProfileInput) (MaxID, error)
	// GetOwnedWalletNamespace returns ErrWalletNotFound unless the wallet is active and owned.
	GetOwnedWalletNamespace(ctx context.Context, walletID, owner uuid.UUID) (chains.Namespace, error)
	SetDestination(ctx context.Context, id, owner uuid.UUID, ns chains.Namespace, walletID uuid.UUID) (MaxID, error)
	SetPastedDestination(ctx context.Context, id, owner uuid.UUID, ns chains.Namespace, address string) (MaxID, error)
	RemoveDestination(ctx context.Context, id, owner uuid.UUID, ns chains.Namespace) (MaxID, error)
	SetSettlement(ctx context.Context, id, owner uuid.UUID, s Settlement) (MaxID, error)
	RemoveSettlement(ctx context.Context, id, owner uuid.UUID) (MaxID, error)
}

type Service interface {
	CheckAvailability(ctx context.Context, name string) (Availability, error)
	Claim(ctx context.Context, caller uuid.UUID, in ClaimInput) (MaxID, error)
	ListMine(ctx context.Context, caller uuid.UUID) ([]MaxID, error)
	Get(ctx context.Context, caller, id uuid.UUID) (MaxID, error)
	UpdateProfile(ctx context.Context, caller, id uuid.UUID, in ProfileInput) (MaxID, error)
	SetDestination(ctx context.Context, caller, id uuid.UUID, namespace string, walletID uuid.UUID) (MaxID, error)
	// PasteDestination sets an owner-confirmed address without a wallet proof.
	PasteDestination(ctx context.Context, caller, id uuid.UUID, namespace, address string, confirmed bool) (MaxID, error)
	RemoveDestination(ctx context.Context, caller, id uuid.UUID, namespace string) (MaxID, error)
	SetSettlement(ctx context.Context, caller, id uuid.UUID, chain, asset string, enabled bool) (MaxID, error)
	RemoveSettlement(ctx context.Context, caller, id uuid.UUID) (MaxID, error)
}
