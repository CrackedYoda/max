-- name: CreateRenameRequest :one
INSERT INTO rename_requests (max_id, owner_account_id, old_name, new_name, status, fee_payment_id)
VALUES ($1, $2, $3, $4, 'awaiting_payment', $5)
RETURNING *;

-- name: GetRenameRequest :one
SELECT * FROM rename_requests WHERE id = $1 AND owner_account_id = $2;

-- name: GetPendingRenameForMaxID :one
SELECT * FROM rename_requests WHERE max_id = $1 AND status = 'awaiting_payment';

-- name: LockRenameRequest :one
SELECT * FROM rename_requests WHERE id = $1 FOR UPDATE;

-- name: ListPendingRenames :many
SELECT * FROM rename_requests WHERE status = 'awaiting_payment' ORDER BY created_at LIMIT $1;

-- name: CloseRenameRequest :one
UPDATE rename_requests
SET status = $2, failure_reason = $3, completed_at = sqlc.narg(completed_at), updated_at = now()
WHERE id = $1 AND status = 'awaiting_payment'
RETURNING *;

-- name: ApplyRename :one
UPDATE max_ids
SET canonical_name = sqlc.arg(new_name),
    rename_available_at = now() + make_interval(secs => sqlc.arg(cooldown_seconds)::int),
    listable_at = now() + make_interval(secs => sqlc.arg(hold_seconds)::int),
    resolution_version = resolution_version + 1,
    updated_at = now()
WHERE id = sqlc.arg(id) AND canonical_name = sqlc.arg(old_name)
RETURNING *;

-- name: QuarantineName :exec
INSERT INTO name_quarantine (canonical_name, max_id, quarantined_until)
VALUES ($1, $2, now() + make_interval(secs => sqlc.arg(quarantine_seconds)::int))
ON CONFLICT (canonical_name) DO UPDATE
SET max_id = EXCLUDED.max_id, quarantined_until = EXCLUDED.quarantined_until, created_at = now();

-- name: ReleaseQuarantine :exec
DELETE FROM name_quarantine WHERE canonical_name = $1;

-- name: InsertMaxIDHistory :exec
INSERT INTO max_id_history (max_id, old_name, new_name, fee_payment_id)
VALUES ($1, $2, $3, $4);

-- name: ListMaxIDHistory :many
SELECT * FROM max_id_history WHERE max_id = $1 ORDER BY changed_at DESC;
