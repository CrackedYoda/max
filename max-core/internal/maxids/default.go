package maxids

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/shared"
)

const createDefaultAttempts = 5

// CreateDefault gives an account its default MAX ID inside the caller's
// transaction. The name comes from namer; a name that is already taken or
// held is retried with a fresh one, up to 5 attempts. A second default for the
// same account returns ErrDefaultExists and is never retried. destWallet, when
// set, becomes the destination for its namespace.
func CreateDefault(ctx context.Context, q *generated.Queries, namer DefaultNamer, owner uuid.UUID, typ Type, destWallet *WalletRef) (MaxID, error) {
	if !typ.Valid() {
		return MaxID{}, ErrInvalidType
	}
	var row generated.MaxID
	created := false
	for attempt := 0; attempt < createDefaultAttempts && !created; attempt++ {
		name, err := namer.Generate()
		if err != nil {
			return MaxID{}, err
		}
		// Serialize with rename reservations and finalizations on this name.
		if err := q.LockName(ctx, name); err != nil {
			return MaxID{}, err
		}
		// Quarantined and pending-rename names are not in max_ids, so check them first.
		usage, err := q.NameUsage(ctx, name)
		if err != nil {
			return MaxID{}, err
		}
		if nameStatus(usage) != "" {
			continue
		}
		row, err = q.CreateDefaultMaxID(ctx, generated.CreateDefaultMaxIDParams{
			CanonicalName: name, OwnerAccountID: owner, Type: string(typ),
		})
		if database.IsNoRows(err) {
			continue // canonical name taken: try another
		}
		if database.IsUniqueViolation(err, "max_ids_one_default") {
			return MaxID{}, ErrDefaultExists
		}
		if err != nil {
			return MaxID{}, err
		}
		created = true
	}
	if !created {
		return MaxID{}, shared.ErrInternal.WithError(fmt.Errorf("default MAX ID: no free name after %d attempts", createDefaultAttempts))
	}

	if err := q.CreateOwnershipEvent(ctx, generated.CreateOwnershipEventParams{
		MaxID: row.ID, NextOwner: owner, EventType: "claimed",
	}); err != nil {
		return MaxID{}, err
	}
	out := MaxID{
		ID: row.ID, CanonicalName: row.CanonicalName, OwnerAccountID: owner, Type: typ,
		Status: row.Status, IsDefault: true, ResolutionVersion: row.ResolutionVersion,
		RenameAvailableAt: row.RenameAvailableAt, CreatedAt: row.CreatedAt,
		Destinations: []Destination{},
	}
	if destWallet != nil {
		if err := q.UpsertDestination(ctx, generated.UpsertDestinationParams{
			MaxID: row.ID, ChainNamespace: string(destWallet.Namespace), WalletID: destWallet.ID,
		}); err != nil {
			return MaxID{}, err
		}
		out.Destinations = append(out.Destinations, Destination{Namespace: chains.Namespace(destWallet.Namespace), WalletID: destWallet.ID})
	}
	if err := audit.Write(ctx, q, audit.Entry{
		Actor: &owner, Action: "maxid.default_created", ResourceType: "max_id", ResourceID: row.ID.String(),
		Metadata: map[string]any{"name": row.CanonicalName, "type": string(typ)},
	}); err != nil {
		return MaxID{}, err
	}
	return out, nil
}
