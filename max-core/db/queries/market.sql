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

-- name: ExpireMarketListings :many
-- The worker's sweep: active listings past expires_at close as cancelled
-- ("expired"), which unblocks renames. SKIP LOCKED lets workers share it.
WITH due AS (
    SELECT l.id FROM market_listings l
    WHERE l.status = 'active' AND l.expires_at IS NOT NULL AND l.expires_at <= sqlc.arg(cutoff)::timestamptz
    ORDER BY l.expires_at
    LIMIT sqlc.arg(row_limit)
    FOR UPDATE SKIP LOCKED
)
UPDATE market_listings m SET status = 'cancelled', status_reason = 'expired', updated_at = now()
FROM due WHERE m.id = due.id
RETURNING m.id, m.max_id, m.seller_account_id;

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
-- Leases submitted and confirming sales due for observation by pushing
-- next_check_at forward under SKIP LOCKED, so concurrent workers never
-- observe the same sale; the observer then sets the real next check.
WITH due AS (
    SELECT s.id FROM market_sales s
    WHERE s.status IN ('submitted', 'confirming') AND s.next_check_at IS NOT NULL AND s.next_check_at <= now()
    ORDER BY s.next_check_at
    LIMIT sqlc.arg(row_limit)
    FOR UPDATE SKIP LOCKED
)
UPDATE market_sales m SET next_check_at = now() + make_interval(secs => sqlc.arg(lease_seconds)::double precision)
FROM due WHERE m.id = due.id
RETURNING m.*;

-- name: ClaimMarketSalesPastTxDeadline :many
-- Submitted and confirming sales whose transaction deadline anchor
-- (the later of the lock end and the submission; created_at only when both
-- are missing) is at or before cutoff and that no worker holds; leased like
-- ClaimDueMarketSales. Mirrors market.txDeadline.
WITH due AS (
    SELECT s.id FROM market_sales s
    WHERE s.status IN ('submitted', 'confirming')
      AND coalesce(greatest(s.lock_expires_at, s.submitted_at), s.created_at) <= sqlc.arg(cutoff)::timestamptz
      AND (s.next_check_at IS NULL OR s.next_check_at <= now())
    ORDER BY s.submitted_at
    LIMIT sqlc.arg(row_limit)
    FOR UPDATE SKIP LOCKED
)
UPDATE market_sales m SET next_check_at = now() + make_interval(secs => sqlc.arg(lease_seconds)::double precision)
FROM due WHERE m.id = due.id
RETURNING m.*;

-- name: LeaseMarketSaleCheck :execrows
-- Re-leases one claimed sale right before it is observed, so a slow batch
-- never outlives the claim's lease; 0 rows means the sale moved on.
UPDATE market_sales SET next_check_at = now() + make_interval(secs => sqlc.arg(lease_seconds)::double precision)
WHERE id = sqlc.arg(id) AND status IN ('submitted', 'confirming');

-- name: ListExpiredLockedMarketSales :many
-- Candidates only: ExpireSale re-checks each under the max_ids and sale locks.
SELECT * FROM market_sales
WHERE status = 'locked' AND tx_hash IS NULL AND lock_expires_at IS NOT NULL AND lock_expires_at <= sqlc.arg(cutoff)::timestamptz
ORDER BY lock_expires_at
LIMIT sqlc.arg(row_limit);

-- name: ListUnlapsedExpiredOfferSales :many
-- Accepted offers whose sale expired unpaid but that never lapsed (the
-- worker stopped between the two steps). LapseAcceptedOffer is idempotent.
SELECT o.id FROM market_offers o
JOIN market_sales s ON s.offer_id = o.id
WHERE o.status = 'accepted' AND s.status = 'expired' AND s.tx_hash IS NULL
ORDER BY s.updated_at
LIMIT sqlc.arg(row_limit);

-- name: AttachLateMarketSaleTx :one
-- A transaction for an expired, unpaid sale (spec §6 Expiry and late
-- payment). to_status is confirming, or manual_review when the name moved on.
UPDATE market_sales
SET status = sqlc.arg(to_status), status_reason = sqlc.arg(reason), tx_hash = sqlc.arg(tx_hash), submitted_at = now(),
    next_check_at = CASE WHEN sqlc.arg(to_status)::text = 'confirming' THEN now() END, updated_at = now()
WHERE id = sqlc.arg(id) AND buyer_account_id = sqlc.arg(buyer) AND status = 'expired' AND tx_hash IS NULL
RETURNING *;

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
SELECT count(*) FROM market_offers WHERE max_id = $1 AND status = 'live' AND expires_at > now();

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

-- Offers (Task 8)

-- name: LockMarketOfferBuyer :exec
-- Serialises offer creation per buyer so the open-offer limit holds.
SELECT pg_advisory_xact_lock(hashtextextended('market_offers:' || sqlc.arg(buyer)::text, 0));

-- name: CountOpenMarketOffersByBuyer :one
SELECT count(*) FROM market_offers WHERE buyer_account_id = $1 AND status IN ('live', 'unfunded');

-- name: HasOpenMarketOfferOnName :one
SELECT EXISTS (SELECT 1 FROM market_offers
               WHERE buyer_account_id = $1 AND max_id = $2 AND status IN ('live', 'unfunded', 'accepted'))::boolean;

-- name: CreateMarketOffer :one
INSERT INTO market_offers (
    max_id, buyer_account_id, from_address, chain, asset_address, asset_decimals,
    amount_base_units, status, expires_at, funded_checked_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'live', $8, now())
RETURNING *;

-- name: GetMarketOffer :one
SELECT * FROM market_offers WHERE id = $1;

-- name: GetMarketOfferForUpdate :one
SELECT * FROM market_offers WHERE id = $1 FOR UPDATE;

-- name: TransitionMarketOffer :one
UPDATE market_offers
SET status = sqlc.arg(to_status), updated_at = now()
WHERE id = sqlc.arg(id) AND status = ANY(sqlc.arg(from_statuses)::text[])
RETURNING *;

-- name: DeclineMarketOffer :one
UPDATE market_offers
SET status = 'declined', decline_note = sqlc.arg(note), updated_at = now()
WHERE id = sqlc.arg(id) AND status IN ('live', 'unfunded')
RETURNING *;

-- name: SetMarketOfferFunding :one
UPDATE market_offers
SET status = sqlc.arg(to_status), funded_checked_at = sqlc.arg(checked_at), updated_at = now()
WHERE id = sqlc.arg(id) AND status IN ('live', 'unfunded')
RETURNING *;

-- name: ClaimMarketOfferFundingChecks :many
-- Claims due offers by stamping funded_checked_at under SKIP LOCKED, so
-- concurrent workers never check the same offer; the RPC runs after commit.
WITH due AS (
    SELECT f.id FROM market_offers f
    WHERE f.status IN ('live', 'unfunded') AND f.expires_at > now()
      AND (f.funded_checked_at IS NULL OR f.funded_checked_at < sqlc.arg(older_than)::timestamptz)
    ORDER BY f.funded_checked_at NULLS FIRST
    LIMIT sqlc.arg(row_limit)
    FOR UPDATE SKIP LOCKED
)
UPDATE market_offers o SET funded_checked_at = now()
FROM due WHERE o.id = due.id
RETURNING o.*;

-- name: ExpireMarketOffers :many
WITH due AS (
    SELECT f.id FROM market_offers f
    WHERE f.status IN ('live', 'unfunded') AND f.expires_at <= now()
    ORDER BY f.expires_at
    LIMIT sqlc.arg(row_limit)
    FOR UPDATE SKIP LOCKED
)
UPDATE market_offers o SET status = 'expired', updated_at = now()
FROM due WHERE o.id = due.id
RETURNING o.id, o.buyer_account_id, o.max_id;

-- name: GetMarketSaleByOfferID :one
SELECT * FROM market_sales WHERE offer_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: ListLiveMarketOffersReceived :many
-- The seller's inbox: only live, unexpired offers on names they own now.
SELECT sqlc.embed(o), m.canonical_name
FROM market_offers o
JOIN max_ids m ON m.id = o.max_id
WHERE m.owner_account_id = $1 AND o.status = 'live' AND o.expires_at > now()
ORDER BY o.created_at DESC
LIMIT $2;

-- name: ListMarketOffersByBuyerWithName :many
SELECT sqlc.embed(o), m.canonical_name
FROM market_offers o
JOIN max_ids m ON m.id = o.max_id
WHERE o.buyer_account_id = $1
ORDER BY o.created_at DESC
LIMIT $2;

-- Strikes and restrictions

-- name: InsertMarketStrike :exec
INSERT INTO market_strikes (account_id, offer_id) VALUES ($1, $2);

-- name: DeleteMarketStrikesByOffer :execrows
-- A lapsed offer that was paid late after all earns no strike.
DELETE FROM market_strikes WHERE offer_id = $1;

-- name: CountMarketStrikesSince :one
SELECT count(*) FROM market_strikes WHERE account_id = $1 AND created_at > $2;

-- name: CreateMarketRestriction :one
INSERT INTO market_restrictions (account_id, kind, until, reason, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetActiveMarketRestriction :one
-- The longest-running active restriction of a kind (no end date first).
SELECT * FROM market_restrictions
WHERE account_id = $1 AND kind = $2 AND lifted_at IS NULL AND (until IS NULL OR until > now())
ORDER BY until DESC NULLS FIRST
LIMIT 1;
