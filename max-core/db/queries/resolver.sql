-- name: GetPublicMaxID :one
SELECT id, canonical_name, type, display_name, description, avatar_url, resolution_version
FROM max_ids
WHERE canonical_name = $1 AND status = 'active';

-- name: ListPublicDestinations :many
SELECT d.chain_namespace, w.address
FROM max_id_destinations d
JOIN wallets w ON w.id = d.wallet_id
WHERE d.max_id = $1 AND w.status = 'active'
ORDER BY d.chain_namespace;
