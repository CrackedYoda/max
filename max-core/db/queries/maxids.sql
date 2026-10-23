-- name: GetReservedName :one
SELECT * FROM reserved_names WHERE canonical_name = $1 AND status = 'reserved';

-- name: MaxIDNameExists :one
SELECT EXISTS (SELECT 1 FROM max_ids WHERE canonical_name = $1);

-- name: CountMaxIDsByOwner :one
SELECT count(*) FROM max_ids WHERE owner_account_id = $1;

-- name: CreateMaxID :one
INSERT INTO max_ids (canonical_name, owner_account_id, type, display_name)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateOwnershipEvent :exec
INSERT INTO max_id_ownership_events (max_id, previous_owner, next_owner, event_type)
VALUES ($1, $2, $3, $4);

-- name: ListMaxIDsByOwner :many
SELECT * FROM max_ids WHERE owner_account_id = $1 ORDER BY created_at;

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
DO UPDATE SET wallet_id = EXCLUDED.wallet_id, updated_at = now();

-- name: DeleteDestination :execrows
DELETE FROM max_id_destinations WHERE max_id = $1 AND chain_namespace = $2;

-- name: BumpResolutionVersion :one
UPDATE max_ids
SET resolution_version = resolution_version + 1, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListDestinations :many
SELECT d.chain_namespace, d.wallet_id, d.updated_at, w.address
FROM max_id_destinations d
JOIN wallets w ON w.id = d.wallet_id
WHERE d.max_id = $1 AND w.status = 'active'
ORDER BY d.chain_namespace;

-- name: GetOwnedActiveWallet :one
SELECT * FROM wallets WHERE id = $1 AND account_id = $2 AND status = 'active';
