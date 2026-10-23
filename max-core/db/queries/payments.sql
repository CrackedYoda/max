-- name: CreatePayment :one
INSERT INTO payments (
    sender_account_id, sender_address, idempotency_key,
    max_id, recipient_handle, recipient_address, resolution_version,
    chain, chain_namespace, asset_kind, asset_address, asset_decimals, asset_symbol, asset_verified,
    token_program, amount_base_units, route_type, status, required_confirmations, expires_at, purpose
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
)
RETURNING *;

-- name: GetPayment :one
SELECT * FROM payments WHERE id = $1;

-- name: GetPaymentByIdempotencyKey :one
SELECT * FROM payments WHERE sender_address = $1 AND idempotency_key = $2;

-- name: ListPaymentsBySender :many
SELECT * FROM payments
WHERE sender_account_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListPaymentsReceived :many
SELECT p.* FROM payments p
JOIN max_ids m ON m.id = p.max_id
WHERE m.owner_account_id = $1
  AND p.status IN ('SUBMITTED', 'CONFIRMING', 'COMPLETED', 'MANUAL_REVIEW')
ORDER BY p.created_at DESC
LIMIT $2;

-- name: TransitionPayment :one
-- The status precondition makes every transition a compare-and-swap, so
-- concurrent workers or retried requests cannot apply the same step twice.
UPDATE payments
SET status        = sqlc.arg(to_status),
    status_reason = sqlc.arg(status_reason),
    tx_hash       = COALESCE(sqlc.narg(tx_hash), tx_hash),
    confirmations = COALESCE(sqlc.narg(confirmations), confirmations),
    submitted_at  = COALESCE(sqlc.narg(submitted_at), submitted_at),
    completed_at  = COALESCE(sqlc.narg(completed_at), completed_at),
    -- Scheduling is relative to the database clock so app/DB clock skew
    -- can never make a row look not-yet-due.
    next_check_at = CASE WHEN sqlc.narg(next_check_in_ms)::bigint IS NULL THEN NULL
                         ELSE now() + make_interval(secs => sqlc.narg(next_check_in_ms)::bigint / 1000.0) END,
    updated_at    = now()
WHERE id = sqlc.arg(id) AND status = sqlc.arg(from_status)
RETURNING *;

-- name: ReschedulePayment :exec
UPDATE payments
SET next_check_at = now() + make_interval(secs => sqlc.arg(next_check_in_ms)::bigint / 1000.0),
    confirmations = sqlc.arg(confirmations),
    updated_at = now()
WHERE id = sqlc.arg(id) AND status = sqlc.arg(status);

-- name: LeaseDuePayments :many
-- Leasing pushes next_check_at forward so other workers skip these rows while
-- this one makes slow RPC calls without holding a transaction open.
UPDATE payments
SET next_check_at = now() + make_interval(secs => sqlc.arg(lease_seconds)::int)
WHERE id IN (
    SELECT id FROM payments
    WHERE status IN ('SUBMITTED', 'CONFIRMING', 'REFUND_PENDING') AND next_check_at <= now()
    ORDER BY next_check_at
    LIMIT sqlc.arg(batch)
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ExpireAwaitingPayments :many
WITH expired AS (
    UPDATE payments p
    SET status = 'EXPIRED', status_reason = 'sender did not submit a transaction before the intent expired', updated_at = now()
    FROM (
        SELECT id, status AS old_status FROM payments
        WHERE status IN ('CREATED', 'AWAITING_SENDER_SIGNATURE', 'QUOTED') AND expires_at <= now()
        FOR UPDATE SKIP LOCKED
    ) due
    WHERE p.id = due.id
    RETURNING p.id, due.old_status
)
SELECT id, old_status FROM expired;

-- name: InsertPaymentEvent :exec
INSERT INTO payment_events (payment_id, event_type, from_status, to_status, detail)
VALUES ($1, $2, $3, $4, $5);

-- name: ListPaymentEvents :many
SELECT * FROM payment_events WHERE payment_id = $1 ORDER BY created_at;

-- name: CreatePaymentRoute :exec
INSERT INTO payment_routes (
    payment_id, provider, swap_type, requested_amount, origin_asset_id, destination_asset_id, destination_chain,
    destination_asset_kind, destination_asset_address, destination_asset_decimals, destination_asset_symbol,
    deposit_address, refund_to, amount_in, amount_out_expected, min_amount_out, amount_in_usd, amount_out_usd,
    slippage_bps, time_estimate_seconds, quote_deadline, quote_signature, correlation_id, quote_raw
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24
);

-- name: GetPaymentRoute :one
SELECT * FROM payment_routes WHERE payment_id = $1;

-- name: MarkRouteDepositConfirmed :execrows
UPDATE payment_routes SET deposit_confirmed_at = now(), updated_at = now()
WHERE payment_id = $1 AND deposit_confirmed_at IS NULL;

-- name: UpdateRouteProgress :exec
UPDATE payment_routes
SET provider_status       = sqlc.arg(provider_status),
    provider_updated_at   = sqlc.narg(provider_updated_at),
    amount_out_actual     = COALESCE(sqlc.narg(amount_out_actual), amount_out_actual),
    refunded_amount       = COALESCE(sqlc.narg(refunded_amount), refunded_amount),
    refund_reason         = COALESCE(sqlc.narg(refund_reason), refund_reason),
    destination_tx_hashes = sqlc.arg(destination_tx_hashes),
    updated_at            = now()
WHERE payment_id = sqlc.arg(payment_id);
