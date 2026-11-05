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

-- Eligibility, parties, guarded transitions and the public read model

-- name: GetMarketNameFacts :one
-- Everything market.Listable needs about one MAX ID, read in the caller's
-- transaction (after LockMaxIDByID when used for a write).
SELECT m.id, m.canonical_name, m.owner_account_id, m.status, a.status AS owner_status,
       m.listable_at, m.rename_available_at,
       EXISTS (SELECT 1 FROM reserved_name_claims c
               WHERE c.max_id = m.id AND c.status IN ('pending_review', 'active', 'lapsed'))::boolean AS has_live_reserved_claim,
       EXISTS (SELECT 1 FROM rename_requests r
               WHERE r.max_id = m.id AND r.status = 'awaiting_payment')::boolean AS rename_pending,
       EXISTS (SELECT 1 FROM market_restrictions x
               WHERE x.account_id = m.owner_account_id AND x.kind = 'ban' AND x.lifted_at IS NULL
                 AND (x.until IS NULL OR x.until > now()))::boolean AS owner_banned
FROM max_ids m
JOIN accounts a ON a.id = m.owner_account_id
WHERE m.id = $1;

-- name: GetMaxIDIDByCanonicalName :one
SELECT id FROM max_ids WHERE canonical_name = $1;

-- name: HasActiveMarketRestriction :one
SELECT EXISTS (SELECT 1 FROM market_restrictions
               WHERE account_id = $1 AND kind = $2 AND lifted_at IS NULL
                 AND (until IS NULL OR until > now()))::boolean;

-- name: GetMarketWalletLinks :one
-- Wallet history in any status: removed wallets still count as shared.
SELECT
    EXISTS (SELECT 1 FROM wallets b
            JOIN wallets s ON s.chain_namespace = b.chain_namespace AND s.address = b.address
            WHERE b.account_id = sqlc.arg(buyer) AND s.account_id = sqlc.arg(seller))::boolean AS shared_wallet,
    EXISTS (SELECT 1 FROM wallets w
            WHERE w.account_id = sqlc.arg(seller) AND w.address = sqlc.arg(payer_address))::boolean AS payer_ever_sellers;

-- name: GetAgentAdministratorID :one
SELECT administrator_account_id FROM agents WHERE account_id = $1;

-- name: UpdateMarketListingPrice :one
UPDATE market_listings
SET price_base_units = $2, updated_at = now()
WHERE id = $1 AND status = 'active'
RETURNING *;

-- name: TransitionMarketListing :one
UPDATE market_listings
SET status = sqlc.arg(to_status), status_reason = sqlc.arg(reason), updated_at = now()
WHERE id = sqlc.arg(id) AND status = sqlc.arg(from_status)
RETURNING *;

-- name: TransitionMarketSale :one
UPDATE market_sales
SET status = sqlc.arg(to_status), status_reason = sqlc.arg(reason), updated_at = now()
WHERE id = sqlc.arg(id) AND status = sqlc.arg(from_status)
RETURNING *;

-- name: AttachMarketSaleTx :one
UPDATE market_sales
SET status = 'submitted', tx_hash = sqlc.arg(tx_hash), submitted_at = now(), next_check_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND buyer_account_id = sqlc.arg(buyer) AND status = 'locked' AND tx_hash IS NULL
RETURNING *;

-- name: ProgressMarketSale :one
UPDATE market_sales
SET status = sqlc.arg(to_status), status_reason = sqlc.arg(reason), confirmations = sqlc.arg(confirmations),
    next_check_at = sqlc.narg(next_check_at), updated_at = now()
WHERE id = sqlc.arg(id) AND status = sqlc.arg(from_status)
RETURNING *;

-- name: CompleteMarketSale :one
UPDATE market_sales
SET status = 'completed', confirmations = sqlc.arg(confirmations), completed_at = now(), next_check_at = NULL, updated_at = now()
WHERE id = sqlc.arg(id) AND status IN ('submitted', 'confirming')
RETURNING *;

-- name: CompleteMarketOffer :exec
UPDATE market_offers SET status = 'completed', updated_at = now()
WHERE id = $1 AND status = 'accepted';

-- name: SupersedeMarketOffers :exec
-- Every other live or unfunded offer on the name once it changes hands.
UPDATE market_offers SET status = 'superseded', updated_at = now()
WHERE max_id = sqlc.arg(max_id) AND status IN ('live', 'unfunded')
  AND id IS DISTINCT FROM sqlc.narg(keep_offer_id);

-- name: SearchMarketListings :many
-- Public listing search. Prices are compared as decimal asset amounts so the
-- filter is independent of each asset's decimals.
SELECT sqlc.embed(l), m.canonical_name
FROM market_listings l
JOIN max_ids m ON m.id = l.max_id
WHERE l.status = 'active'
  AND (l.expires_at IS NULL OR l.expires_at > now())
  AND (sqlc.arg(q)::text = '' OR m.canonical_name LIKE '%' || sqlc.arg(q)::text || '%')
  AND (sqlc.narg(min_price)::text IS NULL
       OR l.price_base_units::numeric / power(10::numeric, l.asset_decimals) >= (sqlc.narg(min_price)::text)::numeric)
  AND (sqlc.narg(max_price)::text IS NULL
       OR l.price_base_units::numeric / power(10::numeric, l.asset_decimals) <= (sqlc.narg(max_price)::text)::numeric)
ORDER BY
  CASE WHEN sqlc.arg(sort)::text = 'price_asc' THEN l.price_base_units::numeric / power(10::numeric, l.asset_decimals) END ASC,
  CASE WHEN sqlc.arg(sort)::text = 'price_desc' THEN l.price_base_units::numeric / power(10::numeric, l.asset_decimals) END DESC,
  l.created_at DESC, l.id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: GetLastCompletedMarketSale :one
SELECT * FROM market_sales
WHERE max_id = $1 AND status = 'completed'
ORDER BY completed_at DESC
LIMIT 1;

-- name: CountLiveMarketOffers :one
SELECT count(*) FROM market_offers WHERE max_id = $1 AND status = 'live';

-- name: ListMarketListingsBySellerWithName :many
SELECT sqlc.embed(l), m.canonical_name
FROM market_listings l
JOIN max_ids m ON m.id = l.max_id
WHERE l.seller_account_id = $1
ORDER BY l.created_at DESC
LIMIT $2;

-- name: ListMarketSalesByPartyWithName :many
SELECT sqlc.embed(s), m.canonical_name
FROM market_sales s
JOIN max_ids m ON m.id = s.max_id
WHERE s.seller_account_id = $1 OR s.buyer_account_id = $1
ORDER BY s.created_at DESC
LIMIT $2;
