-- Listings

-- name: CreateMarketListing :one
INSERT INTO market_listings (
    max_id, seller_account_id, chain, asset_address, asset_decimals,
    price_base_units, payout_address, status, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8)
RETURNING *;

-- name: GetMarketListing :one
SELECT * FROM market_listings WHERE id = $1;

-- name: GetMarketListingForUpdate :one
SELECT * FROM market_listings WHERE id = $1 FOR UPDATE;

-- name: GetLiveMarketListingByMaxID :one
SELECT * FROM market_listings WHERE max_id = $1 AND status IN ('active', 'locked');

-- name: GetLiveMarketListingByMaxIDForUpdate :one
SELECT * FROM market_listings WHERE max_id = $1 AND status IN ('active', 'locked') FOR UPDATE;

-- name: UpdateMarketListingStatus :one
UPDATE market_listings
SET status = $2, status_reason = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListMarketListingsBySeller :many
SELECT * FROM market_listings
WHERE seller_account_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListActiveMarketListings :many
SELECT l.* FROM market_listings l
WHERE l.status = 'active'
  AND (l.expires_at IS NULL OR l.expires_at > now())
ORDER BY l.created_at DESC
LIMIT $1 OFFSET $2;

-- name: ListExpiredMarketListings :many
SELECT * FROM market_listings
WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= now()
ORDER BY expires_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- Sales

-- name: CreateMarketSale :one
INSERT INTO market_sales (
    max_id, listing_id, offer_id, seller_account_id, buyer_account_id, buyer_address,
    seller_payout_address, treasury_address, chain, asset_address, asset_decimals,
    price_base_units, fee_bps, fee_base_units, seller_base_units, quoted_network_fee,
    idempotency_key, status, status_reason, lock_expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
)
RETURNING *;

-- name: GetMarketSale :one
SELECT * FROM market_sales WHERE id = $1;

-- name: GetMarketSaleForUpdate :one
SELECT * FROM market_sales WHERE id = $1 FOR UPDATE;

-- name: GetMarketSaleByIdempotencyKey :one
SELECT * FROM market_sales WHERE buyer_account_id = $1 AND idempotency_key = $2;

-- name: GetOpenMarketSaleByMaxID :one
SELECT * FROM market_sales
WHERE max_id = $1 AND status IN ('pending_review', 'locked', 'submitted', 'confirming');

-- name: GetOpenMarketSaleByMaxIDForUpdate :one
SELECT * FROM market_sales
WHERE max_id = $1 AND status IN ('pending_review', 'locked', 'submitted', 'confirming')
FOR UPDATE;

-- name: UpdateMarketSaleStatus :one
UPDATE market_sales
SET status = $2, status_reason = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SubmitMarketSale :one
UPDATE market_sales
SET status = 'submitted', tx_hash = $2, submitted_at = now(), next_check_at = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateMarketSaleProgress :one
UPDATE market_sales
SET status = $2, status_reason = $3, confirmations = $4, next_check_at = $5,
    completed_at = $6, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListMarketSalesByParty :many
SELECT * FROM market_sales
WHERE seller_account_id = $1 OR buyer_account_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListMarketSalesByMaxID :many
SELECT * FROM market_sales WHERE max_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: ListMarketSalesByStatus :many
SELECT * FROM market_sales WHERE status = $1 ORDER BY created_at LIMIT $2;

-- name: ClaimDueMarketSales :many
SELECT * FROM market_sales
WHERE status IN ('submitted', 'confirming') AND next_check_at IS NOT NULL AND next_check_at <= now()
ORDER BY next_check_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: ListExpiredLockedMarketSales :many
SELECT * FROM market_sales
WHERE status = 'locked' AND lock_expires_at IS NOT NULL AND lock_expires_at <= now()
ORDER BY lock_expires_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- Sale events

-- name: InsertMarketSaleEvent :exec
INSERT INTO market_sale_events (sale_id, event_type, from_status, to_status, detail)
VALUES ($1, $2, $3, $4, $5);

-- name: ListMarketSaleEvents :many
SELECT * FROM market_sale_events WHERE sale_id = $1 ORDER BY created_at;
