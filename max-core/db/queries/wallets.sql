-- name: ListWalletsByAccount :many
SELECT * FROM wallets
WHERE account_id = $1 AND status = 'active'
ORDER BY created_at;

-- name: GetActiveWalletForAccount :one
SELECT * FROM wallets
WHERE id = $1 AND account_id = $2 AND status = 'active';

-- name: CountActiveWalletsByAccount :one
SELECT count(*) FROM wallets WHERE account_id = $1 AND status = 'active';

-- name: LockActiveWalletForAccount :one
SELECT * FROM wallets
WHERE id = $1 AND account_id = $2 AND status = 'active'
FOR UPDATE;

-- name: MarkWalletRemoved :exec
UPDATE wallets SET status = 'removed' WHERE id = $1;

-- name: DeleteDestinationsForWallet :many
DELETE FROM max_id_destinations WHERE wallet_id = $1 RETURNING max_id;

-- name: BumpResolutionVersions :exec
UPDATE max_ids
SET resolution_version = resolution_version + 1, updated_at = now()
WHERE id = ANY(sqlc.arg(ids)::uuid[]);
