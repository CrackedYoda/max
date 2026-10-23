-- name: CreateOAuthState :exec
INSERT INTO oauth_states (state_hash, provider, account_id, session_id, code_verifier, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ConsumeOAuthState :one
UPDATE oauth_states SET used_at = now()
WHERE state_hash = $1 AND used_at IS NULL AND expires_at > now()
RETURNING *;

-- name: GetActiveVerificationForProviderUser :one
SELECT * FROM external_verifications
WHERE provider = $1 AND provider_user_id = $2 AND revoked_at IS NULL
FOR UPDATE;

-- name: CreateVerification :one
INSERT INTO external_verifications (account_id, provider, provider_user_id, username, display_name, provider_created_at, verified_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
RETURNING *;

-- name: RevokeVerification :execrows
UPDATE external_verifications SET revoked_at = now(), revoke_reason = sqlc.arg(reason)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL
  AND (sqlc.narg(account_id)::uuid IS NULL OR account_id = sqlc.narg(account_id));

-- name: ListVerificationsByAccount :many
SELECT * FROM external_verifications WHERE account_id = $1 ORDER BY verified_at DESC;

-- name: GetLatestActiveVerification :one
SELECT * FROM external_verifications
WHERE account_id = $1 AND provider = $2 AND revoked_at IS NULL
ORDER BY verified_at DESC
LIMIT 1;

-- name: LockReservedName :one
SELECT * FROM reserved_names WHERE canonical_name = $1 AND status = 'reserved' FOR UPDATE;

-- name: GetReservedNameByID :one
SELECT * FROM reserved_names WHERE id = $1;

-- name: SetReservedNameStatus :exec
UPDATE reserved_names SET status = $2 WHERE id = $1;

-- name: CreateReservedClaim :one
INSERT INTO reserved_name_claims (
    reserved_name_id, canonical_name, account_id, max_id, verification_id, x_user_id, x_username, status, verified_until
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetReservedClaimForUpdate :one
SELECT * FROM reserved_name_claims WHERE id = $1 FOR UPDATE;

-- name: ListReservedClaimsByAccount :many
SELECT * FROM reserved_name_claims WHERE account_id = $1 ORDER BY created_at DESC;

-- name: ListReservedClaimsByStatus :many
SELECT * FROM reserved_name_claims WHERE status = $1 ORDER BY created_at LIMIT $2;

-- name: DecideReservedClaim :one
UPDATE reserved_name_claims
SET status = sqlc.arg(to_status), review_note = sqlc.arg(note), decided_by = sqlc.narg(decided_by),
    decided_at = now(), max_id = COALESCE(sqlc.narg(max_id), max_id),
    verified_until = COALESCE(sqlc.narg(verified_until), verified_until), updated_at = now()
WHERE id = sqlc.arg(id) AND status = sqlc.arg(from_status)
RETURNING *;

-- name: ExtendReservedClaims :many
-- Re-verification by the same X user id renews every live claim it backs.
UPDATE reserved_name_claims
SET verified_until = sqlc.arg(verified_until), verification_id = sqlc.arg(verification_id),
    x_username = sqlc.arg(x_username), status = 'active', updated_at = now()
WHERE account_id = sqlc.arg(account_id) AND x_user_id = sqlc.arg(x_user_id) AND status IN ('active', 'lapsed')
RETURNING *;

-- name: LapseOverdueClaims :many
UPDATE reserved_name_claims
SET status = 'lapsed', updated_at = now()
WHERE status = 'active' AND verified_until < now() - make_interval(secs => sqlc.arg(grace_seconds)::int)
RETURNING *;

-- name: SetMaxIDStatus :exec
UPDATE max_ids SET status = $2, resolution_version = resolution_version + 1, updated_at = now() WHERE id = $1;
