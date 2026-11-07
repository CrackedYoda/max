ALTER TABLE agent_policies DROP COLUMN IF EXISTS market;

DELETE FROM reserved_names WHERE canonical_name = 'market' AND claim_policy = 'blocked' AND status = 'reserved';

ALTER TABLE max_id_ownership_events DROP COLUMN IF EXISTS sale_id;
ALTER TABLE max_ids DROP COLUMN IF EXISTS listable_at, DROP COLUMN IF EXISTS owner_changed_at;

DROP TABLE IF EXISTS agent_market_authorizations;
DROP TABLE IF EXISTS market_strikes;
DROP TABLE IF EXISTS market_restrictions;
DROP TABLE IF EXISTS market_sale_events;
DROP TRIGGER IF EXISTS market_sales_snapshot_immutable ON market_sales;
DROP TABLE IF EXISTS market_sales;
DROP FUNCTION IF EXISTS market_sales_snapshot_immutable();
DROP TABLE IF EXISTS market_offers;
DROP TABLE IF EXISTS market_listings;
