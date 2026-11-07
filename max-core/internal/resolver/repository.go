package resolver

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/shared"
)

type repository struct {
	q *generated.Queries
}

var _ Repository = (*repository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &repository{q: generated.New(pool)}
}

func (r *repository) GetProfile(ctx context.Context, canonical string) (Profile, error) {
	row, err := r.q.GetPublicMaxID(ctx, canonical)
	if err != nil {
		if database.IsNoRows(err) {
			return Profile{}, ErrMaxIDNotFound
		}
		return Profile{}, shared.ErrInternal.WithError(err)
	}
	dests, err := r.q.ListPublicDestinations(ctx, row.ID)
	if err != nil {
		return Profile{}, shared.ErrInternal.WithError(err)
	}
	p := Profile{
		MaxID: row.ID, CanonicalName: row.CanonicalName, Type: row.Type,
		DisplayName: row.DisplayName, Description: row.Description, AvatarURL: row.AvatarUrl, IsDefault: row.IsDefault,
		ResolutionVersion: row.ResolutionVersion, OwnerChangedAt: row.OwnerChangedAt, Destinations: make(map[chains.Namespace]string, len(dests)),
	}
	for _, d := range dests {
		p.Destinations[chains.Namespace(d.ChainNamespace)] = d.Address
	}

	pref, err := r.q.GetSettlementPreference(ctx, row.ID)
	if err != nil && !database.IsNoRows(err) {
		return Profile{}, shared.ErrInternal.WithError(err)
	}
	if err == nil && pref.Enabled {
		if c, ok := chains.Lookup(pref.DestinationChain); ok {
			a := Asset{Kind: AssetNative}
			if pref.DestinationAsset != "" {
				a = Asset{Kind: AssetToken, Address: chains.DisplayAddress(c.Namespace, pref.DestinationAsset)}
			}
			p.Settlement = &Settlement{Chain: c, Asset: a}
		}
	}
	return p, nil
}
