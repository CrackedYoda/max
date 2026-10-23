-- name: UpsertSettlementPreference :one
INSERT INTO settlement_preferences (max_id, destination_chain, destination_asset, enabled)
VALUES ($1, $2, $3, $4)
ON CONFLICT (max_id) DO UPDATE
SET destination_chain = EXCLUDED.destination_chain,
    destination_asset = EXCLUDED.destination_asset,
    enabled = EXCLUDED.enabled,
    updated_at = now()
RETURNING *;

-- name: DeleteSettlementPreference :execrows
DELETE FROM settlement_preferences WHERE max_id = $1;

-- name: GetSettlementPreference :one
SELECT * FROM settlement_preferences WHERE max_id = $1;
