-- name: GetReservedName :one
SELECT * FROM reserved_names WHERE canonical_name = $1 AND status = 'reserved';

-- name: MaxIDNameExists :one
SELECT EXISTS (SELECT 1 FROM max_ids WHERE canonical_name = $1);

-- name: CountMaxIDsByOwner :one
-- Default IDs do not count toward the per-account cap.
SELECT count(*) FROM max_ids WHERE owner_account_id = $1 AND NOT is_default;

-- name: CreateMaxID :one
INSERT INTO max_ids (canonical_name, owner_account_id, type, display_name)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateOwnershipEvent :exec
INSERT INTO max_id_ownership_events (max_id, previous_owner, next_owner, event_type)
VALUES ($1, $2, $3, $4);

-- name: ListMaxIDsByOwner :many
-- Claimed IDs first (oldest first), the default last: clients that take the first entry get a claimed name.
SELECT * FROM max_ids WHERE owner_account_id = $1 ORDER BY is_default, created_at, id;

-- name: GetMaxIDForOwner :one
SELECT * FROM max_ids WHERE id = $1 AND owner_account_id = $2;

-- name: LockMaxIDForOwner :one
SELECT * FROM max_ids WHERE id = $1 AND owner_account_id = $2 FOR UPDATE;

-- name: UpdateMaxIDProfile :one
UPDATE max_ids
SET display_name = $2, description = $3, avatar_url = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpsertDestination :exec
INSERT INTO max_id_destinations (max_id, chain_namespace, wallet_id)
VALUES ($1, $2, $3)
ON CONFLICT (max_id, chain_namespace)
DO UPDATE SET wallet_id = EXCLUDED.wallet_id, address = NULL, confirmed_at = NULL, updated_at = now();

-- name: UpsertPastedDestination :exec
INSERT INTO max_id_destinations (max_id, chain_namespace, address, confirmed_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (max_id, chain_namespace)
DO UPDATE SET wallet_id = NULL, address = EXCLUDED.address, confirmed_at = EXCLUDED.confirmed_at, updated_at = now();

-- name: DeleteDestination :execrows
DELETE FROM max_id_destinations WHERE max_id = $1 AND chain_namespace = $2;

-- name: BumpResolutionVersion :one
UPDATE max_ids
SET resolution_version = resolution_version + 1, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListDestinations :many
SELECT d.chain_namespace, d.wallet_id, d.updated_at, COALESCE(w.address, d.address)::text AS address
FROM max_id_destinations d
LEFT JOIN wallets w ON w.id = d.wallet_id
WHERE d.max_id = $1 AND (d.wallet_id IS NULL OR w.status = 'active')
ORDER BY d.chain_namespace;

-- name: GetOwnedActiveWallet :one
SELECT * FROM wallets WHERE id = $1 AND account_id = $2 AND status = 'active';

-- name: LockMaxIDByID :one
SELECT * FROM max_ids WHERE id = $1 FOR UPDATE;

-- name: TransferMaxIDOwner :one
-- Moves the ID to a new owner and resets its public identity. The WHERE clause
-- pins the previous owner so a stale caller cannot transfer twice.
UPDATE max_ids m
SET owner_account_id = sqlc.arg(to_owner),
    type = CASE WHEN (SELECT a.type FROM accounts a WHERE a.id = sqlc.arg(to_owner)) = 'agent' THEN 'agent' ELSE 'human' END,
    display_name = '',
    description = '',
    avatar_url = '',
    resolution_version = resolution_version + 1,
    owner_changed_at = now(),
    listable_at = now() + make_interval(secs => sqlc.arg(hold_seconds)::int),
    rename_available_at = greatest(rename_available_at, now() + make_interval(secs => sqlc.arg(hold_seconds)::int)),
    updated_at = now()
WHERE m.id = sqlc.arg(id) AND m.owner_account_id = sqlc.arg(from_owner)
RETURNING m.*;

-- name: DeleteAllDestinations :exec
DELETE FROM max_id_destinations WHERE max_id = $1;

-- name: CreateTransferEvent :exec
INSERT INTO max_id_ownership_events (max_id, previous_owner, next_owner, event_type, sale_id)
VALUES ($1, $2, $3, 'transferred', $4);

-- name: SetListableAfter :exec
UPDATE max_ids SET listable_at = now() + make_interval(secs => sqlc.arg(hold_seconds)::int) WHERE id = sqlc.arg(id);

-- name: CreateDefaultMaxID :one
-- A taken name returns no row (the caller retries with a new name). A second
-- default for the account still raises the max_ids_one_default violation.
-- listable_at and rename_available_at are both pushed to 9999: defaults are
-- never listable or renameable, and the dates keep it so for a binary that
-- predates is_default (for example after a down migration or a rollback).
INSERT INTO max_ids (canonical_name, owner_account_id, type, display_name, is_default, listable_at, rename_available_at)
VALUES ($1, $2, $3, '', true, timestamptz '9999-12-31 00:00:00+00', timestamptz '9999-12-31 00:00:00+00')
ON CONFLICT (canonical_name) DO NOTHING
RETURNING *;

-- name: FillDefaultDestination :one
-- Gives the owner's active default a destination for the namespace only when it
-- has none; an existing destination is never overwritten. No row means nothing
-- was inserted.
INSERT INTO max_id_destinations (max_id, chain_namespace, wallet_id)
SELECT m.id, sqlc.arg(chain_namespace), sqlc.arg(wallet_id)
FROM max_ids m
WHERE m.owner_account_id = sqlc.arg(owner) AND m.is_default AND m.status = 'active'
ON CONFLICT (max_id, chain_namespace) DO NOTHING
RETURNING max_id;

-- name: GetDefaultMaxID :one
SELECT * FROM max_ids WHERE owner_account_id = $1 AND is_default;

-- name: AccountsWithoutDefault :many
-- Keyset page after (after_created_at, after_id). NO KEY UPDATE does not block
-- foreign-key inserts (sessions, wallets, payments) that reference the account.
SELECT a.id, a.type, a.created_at FROM accounts a
WHERE NOT EXISTS (SELECT 1 FROM max_ids m WHERE m.owner_account_id = a.id AND m.is_default)
  AND (a.created_at, a.id) > (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::uuid)
ORDER BY a.created_at, a.id
LIMIT sqlc.arg(batch)
FOR NO KEY UPDATE OF a SKIP LOCKED;

-- name: ListAccountIDRefs :many
-- The default plus every active claimed ID, claimed (oldest first) before the default.
-- Callers derive the default and primary IDs from this in one round trip.
SELECT id, canonical_name, is_default FROM max_ids
WHERE owner_account_id = $1 AND (is_default OR status = 'active')
ORDER BY is_default, created_at, id;
