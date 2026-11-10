package maxids

import (
	"context"
	"errors"
	"time"

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
	hold time.Duration
}

// RepoOption tunes optional repository behaviour.
type RepoOption func(*repository)

// WithHoldPeriod sets how long a newly claimed ID is held before it can be
// listed on the market. Zero or unset means DefaultHoldPeriod.
func WithHoldPeriod(d time.Duration) RepoOption {
	return func(r *repository) { r.hold = d }
}

var _ Repository = (*repository)(nil)

func NewRepository(pool *pgxpool.Pool, opts ...RepoOption) Repository {
	r := &repository{pool: pool, q: generated.New(pool)}
	for _, o := range opts {
		o(r)
	}
	return r
}

func (r *repository) GetReserved(ctx context.Context, canonical string) (*Reserved, error) {
	row, err := r.q.GetReservedName(ctx, canonical)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}
		return nil, shared.ErrInternal.WithError(err)
	}
	res := &Reserved{CanonicalName: row.CanonicalName, Policy: ClaimPolicy(row.ClaimPolicy)}
	if row.XHandle != nil {
		res.XHandle = *row.XHandle
	}
	return res, nil
}

func (r *repository) NameStatus(ctx context.Context, canonical string) (string, error) {
	u, err := r.q.NameUsage(ctx, canonical)
	if err != nil {
		return "", shared.ErrInternal.WithError(err)
	}
	return nameStatus(u), nil
}

func nameStatus(u generated.NameUsageRow) string {
	switch {
	case u.Taken:
		return "taken"
	case u.PendingRename:
		return "pending_rename"
	case u.QuarantinedFor != uuid.Nil:
		return "quarantined"
	}
	return ""
}

func (r *repository) CountByOwner(ctx context.Context, owner uuid.UUID) (int64, error) {
	n, err := r.q.CountMaxIDsByOwner(ctx, owner)
	if err != nil {
		return 0, shared.ErrInternal.WithError(err)
	}
	return n, nil
}

func (r *repository) ListActiveWallets(ctx context.Context, owner uuid.UUID) ([]WalletRef, error) {
	rows, err := r.q.ListWalletsByAccount(ctx, owner)
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]WalletRef, 0, len(rows))
	for _, w := range rows {
		out = append(out, WalletRef{ID: w.ID, Namespace: chains.Namespace(w.ChainNamespace), Address: w.Address})
	}
	return out, nil
}

func (r *repository) RecordGateCheck(ctx context.Context, owner uuid.UUID, requiredUSD float64, countedUSD *float64, passed bool) error {
	meta := map[string]any{"requiredUsd": requiredUSD, "passed": passed}
	if countedUSD != nil {
		meta["countedUsd"] = *countedUSD
	}
	return audit.Write(ctx, r.q, audit.Entry{
		Actor: &owner, Action: "registration_gate", ResourceType: "account", ResourceID: owner.String(), Metadata: meta,
	})
}

func (r *repository) Create(ctx context.Context, owner uuid.UUID, canonical string, typ Type, displayName string) (MaxID, error) {
	var out MaxID
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := CreateInTx(ctx, q, owner, canonical, typ, displayName, "maxid.claimed", r.hold)
		if err != nil {
			return err
		}
		out, err = r.hydrate(ctx, q, row)
		return err
	})
	if err != nil {
		return MaxID{}, MapCreateErr(err)
	}
	return out, nil
}

// CreateInTx registers a MAX ID inside the caller's transaction: it takes the
// name lock, re-checks availability, inserts the ID and its ownership event,
// starts the market hold (holdPeriod; zero means DefaultHoldPeriod),
// points it at the owner's first wallet per namespace, and audits. Reserved
// name claims call this after their own policy checks.
func CreateInTx(ctx context.Context, q *generated.Queries, owner uuid.UUID, canonical string, typ Type, displayName, auditAction string, holdPeriod time.Duration) (generated.MaxID, error) {
	// Serialize with rename reservations and finalizations on this name.
	if err := q.LockName(ctx, canonical); err != nil {
		return generated.MaxID{}, err
	}
	usage, err := q.NameUsage(ctx, canonical)
	if err != nil {
		return generated.MaxID{}, err
	}
	switch nameStatus(usage) {
	case "taken", "pending_rename":
		return generated.MaxID{}, ErrNameTaken
	case "quarantined":
		return generated.MaxID{}, ErrNameQuarantined
	}

	row, err := q.CreateMaxID(ctx, generated.CreateMaxIDParams{
		CanonicalName: canonical, OwnerAccountID: owner, Type: string(typ), DisplayName: displayName,
	})
	if err != nil {
		return generated.MaxID{}, err
	}
	if err := q.CreateOwnershipEvent(ctx, generated.CreateOwnershipEventParams{
		MaxID: row.ID, NextOwner: owner, EventType: "claimed",
	}); err != nil {
		return generated.MaxID{}, err
	}
	if err := q.SetListableAfter(ctx, generated.SetListableAfterParams{ID: row.ID, HoldSeconds: holdSeconds(holdPeriod)}); err != nil {
		return generated.MaxID{}, err
	}

	wallets, err := q.ListWalletsByAccount(ctx, owner)
	if err != nil {
		return generated.MaxID{}, err
	}
	seen := map[string]bool{}
	for _, w := range wallets {
		if seen[w.ChainNamespace] {
			continue
		}
		seen[w.ChainNamespace] = true
		if err := q.UpsertDestination(ctx, generated.UpsertDestinationParams{
			MaxID: row.ID, ChainNamespace: w.ChainNamespace, WalletID: &w.ID,
		}); err != nil {
			return generated.MaxID{}, err
		}
	}

	return row, audit.Write(ctx, q, audit.Entry{
		Actor: &owner, Action: auditAction, ResourceType: "max_id", ResourceID: row.ID.String(),
		Metadata: map[string]any{"name": canonical, "type": string(typ)},
	})
}

// MapCreateErr turns CreateInTx failures into client-safe errors.
func MapCreateErr(err error) error {
	if database.IsUniqueViolation(err, "max_ids_canonical_name_key") {
		return ErrNameTaken
	}
	var appErr *shared.AppError
	if errors.As(err, &appErr) {
		return err
	}
	return shared.ErrInternal.WithError(err)
}

func (r *repository) ListByOwner(ctx context.Context, owner uuid.UUID) ([]MaxID, error) {
	rows, err := r.q.ListMaxIDsByOwner(ctx, owner)
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]MaxID, 0, len(rows))
	for _, row := range rows {
		m, err := r.hydrate(ctx, r.q, row)
		if err != nil {
			return nil, shared.ErrInternal.WithError(err)
		}
		out = append(out, m)
	}
	return out, nil
}

func (r *repository) GetForOwner(ctx context.Context, id, owner uuid.UUID) (MaxID, error) {
	row, err := r.q.GetMaxIDForOwner(ctx, generated.GetMaxIDForOwnerParams{ID: id, OwnerAccountID: owner})
	if err != nil {
		if database.IsNoRows(err) {
			return MaxID{}, ErrMaxIDNotFound
		}
		return MaxID{}, shared.ErrInternal.WithError(err)
	}
	m, err := r.hydrate(ctx, r.q, row)
	if err != nil {
		return MaxID{}, shared.ErrInternal.WithError(err)
	}
	return m, nil
}

func (r *repository) UpdateProfile(ctx context.Context, id, owner uuid.UUID, in ProfileInput) (MaxID, error) {
	var out MaxID
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockMaxIDForOwner(ctx, generated.LockMaxIDForOwnerParams{ID: id, OwnerAccountID: owner}); err != nil {
			return err
		}
		if err := ensureNoOpenSale(ctx, q, id); err != nil {
			return err
		}
		row, err := q.UpdateMaxIDProfile(ctx, generated.UpdateMaxIDProfileParams{
			ID: id, DisplayName: in.DisplayName, Description: in.Description, AvatarUrl: in.AvatarURL,
		})
		if err != nil {
			return err
		}
		out, err = r.hydrate(ctx, q, row)
		return err
	})
	return out, r.mapOwnedErr(err)
}

func (r *repository) GetOwnedWalletNamespace(ctx context.Context, walletID, owner uuid.UUID) (chains.Namespace, error) {
	w, err := r.q.GetOwnedActiveWallet(ctx, generated.GetOwnedActiveWalletParams{ID: walletID, AccountID: owner})
	if err != nil {
		if database.IsNoRows(err) {
			return "", ErrWalletNotFound
		}
		return "", shared.ErrInternal.WithError(err)
	}
	return chains.Namespace(w.ChainNamespace), nil
}

func (r *repository) SetDestination(ctx context.Context, id, owner uuid.UUID, ns chains.Namespace, walletID uuid.UUID) (MaxID, error) {
	var out MaxID
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		locked, err := q.LockMaxIDForOwner(ctx, generated.LockMaxIDForOwnerParams{ID: id, OwnerAccountID: owner})
		if err != nil {
			return err
		}
		if err := ensureNoOpenSale(ctx, q, id); err != nil {
			return err
		}
		if locked.Status != "active" {
			return ErrMaxIDNotActive
		}
		if err := q.UpsertDestination(ctx, generated.UpsertDestinationParams{
			MaxID: id, ChainNamespace: string(ns), WalletID: &walletID,
		}); err != nil {
			return err
		}
		row, err := q.BumpResolutionVersion(ctx, id)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, q, audit.Entry{
			Actor: &owner, Action: "maxid.destination_set", ResourceType: "max_id", ResourceID: id.String(),
			Metadata: map[string]any{"namespace": string(ns), "source": "wallet", "wallet_id": walletID.String(), "resolution_version": row.ResolutionVersion},
		}); err != nil {
			return err
		}
		out, err = r.hydrate(ctx, q, row)
		return err
	})
	return out, r.mapOwnedErr(err)
}

func (r *repository) SetPastedDestination(ctx context.Context, id, owner uuid.UUID, ns chains.Namespace, address string) (MaxID, error) {
	var out MaxID
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		locked, err := q.LockMaxIDForOwner(ctx, generated.LockMaxIDForOwnerParams{ID: id, OwnerAccountID: owner})
		if err != nil {
			return err
		}
		if err := ensureNoOpenSale(ctx, q, id); err != nil {
			return err
		}
		if locked.Status != "active" {
			return ErrMaxIDNotActive
		}
		if err := q.UpsertPastedDestination(ctx, generated.UpsertPastedDestinationParams{
			MaxID: id, ChainNamespace: string(ns), Address: &address,
		}); err != nil {
			return err
		}
		row, err := q.BumpResolutionVersion(ctx, id)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, q, audit.Entry{
			Actor: &owner, Action: "maxid.destination_set", ResourceType: "max_id", ResourceID: id.String(),
			Metadata: map[string]any{"namespace": string(ns), "source": "pasted", "confirmed": true, "resolution_version": row.ResolutionVersion},
		}); err != nil {
			return err
		}
		out, err = r.hydrate(ctx, q, row)
		return err
	})
	return out, r.mapOwnedErr(err)
}

func (r *repository) SetChainDestination(ctx context.Context, id, owner uuid.UUID, chainKey string, walletID *uuid.UUID, address string) (MaxID, error) {
	var out MaxID
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		locked, err := q.LockMaxIDForOwner(ctx, generated.LockMaxIDForOwnerParams{ID: id, OwnerAccountID: owner})
		if err != nil {
			return err
		}
		if err := ensureNoOpenSale(ctx, q, id); err != nil {
			return err
		}
		if locked.Status != "active" {
			return ErrMaxIDNotActive
		}
		meta := map[string]any{"namespace": "evm", "chain": chainKey}
		if walletID != nil {
			meta["source"], meta["wallet_id"] = "wallet", walletID.String()
			err = q.UpsertChainDestination(ctx, generated.UpsertChainDestinationParams{MaxID: id, ChainKey: &chainKey, WalletID: walletID})
		} else {
			meta["source"], meta["confirmed"] = "pasted", true
			err = q.UpsertChainPastedDestination(ctx, generated.UpsertChainPastedDestinationParams{MaxID: id, ChainKey: &chainKey, Address: &address})
		}
		if err != nil {
			return err
		}
		row, err := q.BumpResolutionVersion(ctx, id)
		if err != nil {
			return err
		}
		meta["resolution_version"] = row.ResolutionVersion
		if err := audit.Write(ctx, q, audit.Entry{
			Actor: &owner, Action: "maxid.destination_set", ResourceType: "max_id", ResourceID: id.String(), Metadata: meta,
		}); err != nil {
			return err
		}
		out, err = r.hydrate(ctx, q, row)
		return err
	})
	return out, r.mapOwnedErr(err)
}

func (r *repository) RemoveChainDestination(ctx context.Context, id, owner uuid.UUID, chainKey string) (MaxID, error) {
	var out MaxID
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockMaxIDForOwner(ctx, generated.LockMaxIDForOwnerParams{ID: id, OwnerAccountID: owner}); err != nil {
			return err
		}
		if err := ensureNoOpenSale(ctx, q, id); err != nil {
			return err
		}
		n, err := q.DeleteChainDestination(ctx, generated.DeleteChainDestinationParams{MaxID: id, ChainKey: &chainKey})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrDestinationNotFound
		}
		row, err := q.BumpResolutionVersion(ctx, id)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, q, audit.Entry{
			Actor: &owner, Action: "maxid.destination_removed", ResourceType: "max_id", ResourceID: id.String(),
			Metadata: map[string]any{"namespace": "evm", "chain": chainKey, "resolution_version": row.ResolutionVersion},
		}); err != nil {
			return err
		}
		out, err = r.hydrate(ctx, q, row)
		return err
	})
	return out, r.mapOwnedErr(err)
}

func (r *repository) RemoveDestination(ctx context.Context, id, owner uuid.UUID, ns chains.Namespace) (MaxID, error) {
	var out MaxID
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockMaxIDForOwner(ctx, generated.LockMaxIDForOwnerParams{ID: id, OwnerAccountID: owner}); err != nil {
			return err
		}
		if err := ensureNoOpenSale(ctx, q, id); err != nil {
			return err
		}
		n, err := q.DeleteDestination(ctx, generated.DeleteDestinationParams{MaxID: id, ChainNamespace: string(ns)})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrDestinationNotFound
		}
		row, err := q.BumpResolutionVersion(ctx, id)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, q, audit.Entry{
			Actor: &owner, Action: "maxid.destination_removed", ResourceType: "max_id", ResourceID: id.String(),
			Metadata: map[string]any{"namespace": string(ns), "resolution_version": row.ResolutionVersion},
		}); err != nil {
			return err
		}
		out, err = r.hydrate(ctx, q, row)
		return err
	})
	return out, r.mapOwnedErr(err)
}

func (r *repository) SetSettlement(ctx context.Context, id, owner uuid.UUID, st Settlement) (MaxID, error) {
	var out MaxID
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockMaxIDForOwner(ctx, generated.LockMaxIDForOwnerParams{ID: id, OwnerAccountID: owner}); err != nil {
			return err
		}
		if err := ensureNoOpenSale(ctx, q, id); err != nil {
			return err
		}
		if _, err := q.UpsertSettlementPreference(ctx, generated.UpsertSettlementPreferenceParams{
			MaxID: id, DestinationChain: st.Chain, DestinationAsset: st.Asset, Enabled: st.Enabled,
		}); err != nil {
			return err
		}
		row, err := q.BumpResolutionVersion(ctx, id)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, q, audit.Entry{
			Actor: &owner, Action: "maxid.settlement_set", ResourceType: "max_id", ResourceID: id.String(),
			Metadata: map[string]any{"chain": st.Chain, "asset": st.Asset, "enabled": st.Enabled},
		}); err != nil {
			return err
		}
		out, err = r.hydrate(ctx, q, row)
		return err
	})
	return out, r.mapOwnedErr(err)
}

func (r *repository) RemoveSettlement(ctx context.Context, id, owner uuid.UUID) (MaxID, error) {
	var out MaxID
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockMaxIDForOwner(ctx, generated.LockMaxIDForOwnerParams{ID: id, OwnerAccountID: owner}); err != nil {
			return err
		}
		if err := ensureNoOpenSale(ctx, q, id); err != nil {
			return err
		}
		n, err := q.DeleteSettlementPreference(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrSettlementNotFound
		}
		row, err := q.BumpResolutionVersion(ctx, id)
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, q, audit.Entry{
			Actor: &owner, Action: "maxid.settlement_removed", ResourceType: "max_id", ResourceID: id.String(),
		}); err != nil {
			return err
		}
		out, err = r.hydrate(ctx, q, row)
		return err
	})
	return out, r.mapOwnedErr(err)
}

func (r *repository) hydrate(ctx context.Context, q *generated.Queries, row generated.MaxID) (MaxID, error) {
	dests, err := q.ListDestinations(ctx, row.ID)
	if err != nil {
		return MaxID{}, err
	}
	m := MaxID{
		ID: row.ID, CanonicalName: row.CanonicalName, OwnerAccountID: row.OwnerAccountID,
		Type: Type(row.Type), Status: row.Status, IsDefault: row.IsDefault, DisplayName: row.DisplayName,
		Description: row.Description, AvatarURL: row.AvatarUrl,
		ResolutionVersion: row.ResolutionVersion, RenameAvailableAt: row.RenameAvailableAt,
		CreatedAt: row.CreatedAt, Destinations: make([]Destination, 0, len(dests)),
	}
	for _, d := range dests {
		m.Destinations = append(m.Destinations, Destination{
			Namespace: chains.Namespace(d.ChainNamespace), ChainKey: deref(d.ChainKey), WalletID: d.WalletID,
			Address: d.Address, Pasted: d.WalletID == nil, UpdatedAt: d.UpdatedAt,
		})
	}
	pref, err := q.GetSettlementPreference(ctx, row.ID)
	if err == nil {
		m.Settlement = &Settlement{Chain: pref.DestinationChain, Asset: pref.DestinationAsset, Enabled: pref.Enabled, UpdatedAt: pref.UpdatedAt}
	} else if !database.IsNoRows(err) {
		return MaxID{}, err
	}
	return m, nil
}

// mapOwnedErr turns "no row for this id+owner" into a 404 so the existence of
// other accounts' IDs is never leaked.
func (r *repository) mapOwnedErr(err error) error {
	if err == nil {
		return nil
	}
	if database.IsNoRows(err) {
		return ErrMaxIDNotFound
	}
	var appErr *shared.AppError
	if errors.As(err, &appErr) {
		return err
	}
	return shared.ErrInternal.WithError(err)
}

// ensureNoOpenSale refuses with ErrNameLocked while the ID has a sale in
// pending_review, locked, submitted or confirming. Call it after taking the
// max_ids row lock, which sale creation also takes, so the check cannot race.
func ensureNoOpenSale(ctx context.Context, q *generated.Queries, id uuid.UUID) error {
	if _, err := q.GetOpenMarketSaleByMaxID(ctx, id); err != nil {
		if database.IsNoRows(err) {
			return nil
		}
		return err
	}
	return ErrNameLocked
}

// EnsureNotListed refuses with ErrNameListed while the ID has an active or
// locked listing. Same locking contract as ensureNoOpenSale.
func EnsureNotListed(ctx context.Context, q *generated.Queries, id uuid.UUID) error {
	if _, err := q.GetLiveMarketListingByMaxID(ctx, id); err != nil {
		if database.IsNoRows(err) {
			return nil
		}
		return err
	}
	return ErrNameListed
}

// EnsureNoOpenSale is the exported form of ensureNoOpenSale for other packages
// that edit a name's identity (renames).
func EnsureNoOpenSale(ctx context.Context, q *generated.Queries, id uuid.UUID) error {
	return ensureNoOpenSale(ctx, q, id)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
