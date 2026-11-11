-- name: CreateAgent :one
INSERT INTO agents (account_id, administrator_account_id, name, description)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateAgentPolicy :one
INSERT INTO agent_policies (agent_account_id) VALUES ($1) RETURNING *;

-- name: GetAgentForAdministrator :one
SELECT * FROM agents WHERE account_id = $1 AND administrator_account_id = $2;

-- name: GetAgent :one
SELECT * FROM agents WHERE account_id = $1;

-- name: ListAgentsByAdministrator :many
SELECT * FROM agents WHERE administrator_account_id = $1 ORDER BY created_at;

-- name: UpdateAgent :one
UPDATE agents SET name = $2, description = $3, status = $4, updated_at = now()
WHERE account_id = $1
RETURNING *;

-- name: GetAgentPolicy :one
SELECT * FROM agent_policies WHERE agent_account_id = $1;

-- name: UpdateAgentPolicy :one
UPDATE agent_policies
SET limits = $2, allowed_recipients = $3, allow_any_recipient = $4, market = $5, version = version + 1, updated_at = now()
WHERE agent_account_id = $1
RETURNING *;

-- name: CreateAgentCredential :one
INSERT INTO agent_credentials (agent_account_id, name, prefix, secret_hash, scopes, created_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetCredentialByPrefix :one
SELECT c.*, a.status AS agent_status
FROM agent_credentials c
JOIN agents a ON a.account_id = c.agent_account_id
WHERE c.prefix = $1;

-- name: ListAgentCredentials :many
SELECT * FROM agent_credentials WHERE agent_account_id = $1 ORDER BY created_at DESC;

-- name: RevokeAgentCredential :execrows
UPDATE agent_credentials SET revoked_at = now(), replaced_by = sqlc.narg(replaced_by)
WHERE id = sqlc.arg(id) AND agent_account_id = sqlc.arg(agent_account_id) AND revoked_at IS NULL;

-- name: TouchAgentCredential :exec
-- Throttled so authentication doesn't write on every request.
UPDATE agent_credentials SET last_used_at = now()
WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: LockAgentBudget :exec
-- Serializes policy evaluation per agent so concurrent proposals cannot
-- jointly exceed a rolling budget.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(agent_key)::text, 0));

-- name: ListAgentSpend24h :many
-- Committed spend over the rolling window: approved transfers plus approved
-- market buys and offer payments, so splitting a budget across transfers and
-- the marketplace cannot exceed it. Transfers come back with their payment
-- status and failure event; agents.reservationHeld decides which still count.
-- A market authorization whose action failed, or whose sale expired or was
-- cancelled, does not count.
SELECT a.chain, a.asset_address, a.amount_base_units, p.status AS payment_status,
       coalesce((SELECT e.event_type FROM payment_events e
                 WHERE e.payment_id = p.id AND e.to_status = 'FAILED'
                 ORDER BY e.created_at DESC LIMIT 1), '')::text AS failure_event
FROM agent_transfer_authorizations a
JOIN payments p ON p.id = a.payment_id
WHERE a.agent_account_id = $1
  AND a.decision IN ('auto_approved', 'approved')
  AND a.created_at > now() - interval '24 hours'
UNION ALL
SELECT m.chain, m.asset_address, m.amount_base_units, ''::text, ''::text
FROM agent_market_authorizations m
LEFT JOIN market_sales s ON s.id = m.sale_id
WHERE m.agent_account_id = $1
  AND m.action IN ('buy', 'pay')
  AND m.decision IN ('auto_approved', 'approved')
  AND m.failure IS NULL
  AND coalesce(m.decided_at, m.created_at) > now() - interval '24 hours'
  AND (s.id IS NULL OR s.status NOT IN ('expired', 'cancelled'));

-- name: CreateTransferAuthorization :one
INSERT INTO agent_transfer_authorizations (
    payment_id, agent_account_id, credential_id, decision, reasons, policy_version,
    chain, asset_address, amount_base_units, instruction
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetTransferAuthorization :one
SELECT * FROM agent_transfer_authorizations WHERE payment_id = $1;

-- name: DecideTransferAuthorization :one
UPDATE agent_transfer_authorizations
SET decision = $2, decided_by = $3, decided_at = now()
WHERE payment_id = $1 AND decision = 'pending_approval'
RETURNING *;

-- name: ListTransferAuthorizations :many
SELECT * FROM agent_transfer_authorizations
WHERE agent_account_id = $1 AND (sqlc.narg(decision)::text IS NULL OR decision = sqlc.narg(decision))
ORDER BY created_at DESC
LIMIT $2;
