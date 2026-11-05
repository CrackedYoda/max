-- Name marketplace (spec §4). MAX never holds funds: sales are snapshots that
-- track a buyer-signed on-chain transfer.

-- Fail loudly rather than renaming a live name; an operator must decide.
DO $$ BEGIN IF EXISTS (SELECT 1 FROM max_ids WHERE canonical_name = 'market')
THEN RAISE EXCEPTION 'a MAX ID named market exists; resolve before migrating'; END IF; END $$;

CREATE TABLE market_listings (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    max_id             uuid NOT NULL REFERENCES max_ids(id),
    seller_account_id  uuid NOT NULL REFERENCES accounts(id),
    chain              text NOT NULL,
    asset_address      text NOT NULL,
    asset_decimals     integer NOT NULL CHECK (asset_decimals >= 0),
    price_base_units   text NOT NULL CHECK (price_base_units ~ '^[1-9][0-9]{0,77}$'),
    payout_address     text NOT NULL,
    status             text NOT NULL CHECK (status IN ('active', 'locked', 'sold', 'cancelled', 'admin_delisted')),
    status_reason      text NOT NULL DEFAULT '',
    expires_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX market_listings_one_live_idx ON market_listings (max_id) WHERE status IN ('active', 'locked');
CREATE INDEX market_listings_seller_idx ON market_listings (seller_account_id, created_at DESC);
CREATE INDEX market_listings_active_idx ON market_listings (created_at DESC) WHERE status = 'active';

CREATE TABLE market_offers (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    max_id             uuid NOT NULL REFERENCES max_ids(id),
    buyer_account_id   uuid NOT NULL REFERENCES accounts(id),
    from_address       text NOT NULL,
    chain              text NOT NULL,
    asset_address      text NOT NULL,
    asset_decimals     integer NOT NULL CHECK (asset_decimals >= 0),
    amount_base_units  text NOT NULL CHECK (amount_base_units ~ '^[1-9][0-9]{0,77}$'),
    status             text NOT NULL CHECK (status IN ('live', 'unfunded', 'accepted', 'declined', 'superseded', 'expired', 'withdrawn', 'lapsed', 'completed')),
    decline_note       text NOT NULL DEFAULT '',
    expires_at         timestamptz NOT NULL,
    funded_checked_at  timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX market_offers_one_per_buyer_idx ON market_offers (buyer_account_id, max_id) WHERE status IN ('live', 'unfunded', 'accepted');
CREATE INDEX market_offers_funding_idx ON market_offers (status, funded_checked_at);
CREATE INDEX market_offers_max_id_idx ON market_offers (max_id, status);
CREATE INDEX market_offers_buyer_idx ON market_offers (buyer_account_id, created_at DESC);

CREATE TABLE market_sales (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    max_id                 uuid NOT NULL REFERENCES max_ids(id),
    listing_id             uuid REFERENCES market_listings(id),
    offer_id               uuid REFERENCES market_offers(id),
    seller_account_id      uuid NOT NULL REFERENCES accounts(id),
    buyer_account_id       uuid NOT NULL REFERENCES accounts(id),
    buyer_address          text NOT NULL,
    seller_payout_address  text NOT NULL,
    treasury_address       text NOT NULL,
    chain                  text NOT NULL,
    asset_address          text NOT NULL,
    asset_decimals         integer NOT NULL CHECK (asset_decimals >= 0),
    price_base_units       text NOT NULL CHECK (price_base_units ~ '^[1-9][0-9]{0,77}$'),
    fee_bps                integer NOT NULL CHECK (fee_bps BETWEEN 0 AND 5000),
    fee_base_units         text NOT NULL CHECK (fee_base_units ~ '^[0-9]{1,78}$'),
    seller_base_units      text NOT NULL CHECK (seller_base_units ~ '^[0-9]{1,78}$'),
    quoted_network_fee     text NOT NULL DEFAULT '0',
    idempotency_key        text NOT NULL,
    status                 text NOT NULL CHECK (status IN ('pending_review', 'locked', 'submitted', 'confirming', 'completed', 'expired', 'cancelled', 'manual_review', 'reversed')),
    status_reason          text NOT NULL DEFAULT '',
    lock_expires_at        timestamptz,
    tx_hash                text,
    confirmations          bigint NOT NULL DEFAULT 0,
    submitted_at           timestamptz,
    completed_at           timestamptz,
    next_check_at          timestamptz,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT market_sales_one_source CHECK ((listing_id IS NULL) <> (offer_id IS NULL)),
    CONSTRAINT market_sales_split CHECK (fee_base_units::numeric + seller_base_units::numeric = price_base_units::numeric)
);
CREATE UNIQUE INDEX market_sales_idempotency_idx ON market_sales (buyer_account_id, idempotency_key);
CREATE UNIQUE INDEX market_sales_one_open_idx ON market_sales (max_id) WHERE status IN ('pending_review', 'locked', 'submitted', 'confirming');
CREATE UNIQUE INDEX market_sales_tx_hash_idx ON market_sales (chain, tx_hash) WHERE tx_hash IS NOT NULL;
CREATE INDEX market_sales_buyer_idx ON market_sales (buyer_account_id, created_at DESC);
CREATE INDEX market_sales_seller_idx ON market_sales (seller_account_id, created_at DESC);
CREATE INDEX market_sales_due_idx ON market_sales (next_check_at) WHERE status IN ('submitted', 'confirming');
CREATE INDEX market_sales_lock_idx ON market_sales (lock_expires_at) WHERE status = 'locked';

-- The snapshot is the authorization record: parties, addresses, asset and
-- amounts never change once written, even if application code has a bug.
CREATE FUNCTION market_sales_snapshot_immutable() RETURNS trigger AS $$
BEGIN
    IF NEW.max_id                 IS DISTINCT FROM OLD.max_id
    OR NEW.listing_id             IS DISTINCT FROM OLD.listing_id
    OR NEW.offer_id               IS DISTINCT FROM OLD.offer_id
    OR NEW.seller_account_id      IS DISTINCT FROM OLD.seller_account_id
    OR NEW.buyer_account_id       IS DISTINCT FROM OLD.buyer_account_id
    OR NEW.buyer_address          IS DISTINCT FROM OLD.buyer_address
    OR NEW.seller_payout_address  IS DISTINCT FROM OLD.seller_payout_address
    OR NEW.treasury_address       IS DISTINCT FROM OLD.treasury_address
    OR NEW.chain                  IS DISTINCT FROM OLD.chain
    OR NEW.asset_address          IS DISTINCT FROM OLD.asset_address
    OR NEW.asset_decimals         IS DISTINCT FROM OLD.asset_decimals
    OR NEW.price_base_units       IS DISTINCT FROM OLD.price_base_units
    OR NEW.fee_bps                IS DISTINCT FROM OLD.fee_bps
    OR NEW.fee_base_units         IS DISTINCT FROM OLD.fee_base_units
    OR NEW.seller_base_units      IS DISTINCT FROM OLD.seller_base_units
    OR (OLD.tx_hash IS NOT NULL AND NEW.tx_hash IS DISTINCT FROM OLD.tx_hash) THEN
        RAISE EXCEPTION 'market sale snapshot is immutable' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.fee_base_units::numeric + NEW.seller_base_units::numeric <> NEW.price_base_units::numeric THEN
        RAISE EXCEPTION 'market sale fee + seller amount must equal price' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER market_sales_snapshot_immutable
    BEFORE UPDATE ON market_sales
    FOR EACH ROW EXECUTE FUNCTION market_sales_snapshot_immutable();

CREATE TABLE market_sale_events (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_id      uuid NOT NULL REFERENCES market_sales(id),
    event_type   text NOT NULL,
    from_status  text,
    to_status    text NOT NULL,
    detail       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX market_sale_events_sale_idx ON market_sale_events (sale_id, created_at);

CREATE TABLE market_restrictions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  uuid NOT NULL REFERENCES accounts(id),
    kind        text NOT NULL CHECK (kind IN ('ban', 'offer_suspension')),
    until       timestamptz,
    reason      text NOT NULL DEFAULT '',
    created_by  uuid REFERENCES accounts(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    lifted_at   timestamptz
);
CREATE INDEX market_restrictions_account_idx ON market_restrictions (account_id, kind) WHERE lifted_at IS NULL;

CREATE TABLE market_strikes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  uuid NOT NULL REFERENCES accounts(id),
    offer_id    uuid REFERENCES market_offers(id),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX market_strikes_account_idx ON market_strikes (account_id, created_at DESC);

CREATE TABLE agent_market_authorizations (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_account_id   uuid NOT NULL REFERENCES agents(account_id),
    credential_id      uuid NOT NULL REFERENCES agent_credentials(id),
    action             text NOT NULL CHECK (action IN ('buy', 'offer', 'list', 'accept')),
    sale_id            uuid REFERENCES market_sales(id),
    offer_id           uuid REFERENCES market_offers(id),
    listing_id         uuid REFERENCES market_listings(id),
    decision           text NOT NULL CHECK (decision IN ('auto_approved', 'pending_approval', 'approved', 'denied')),
    reasons            jsonb NOT NULL DEFAULT '[]'::jsonb,
    policy_version     integer NOT NULL,
    chain              text NOT NULL DEFAULT '',
    asset_address      text NOT NULL DEFAULT '',
    amount_base_units  text NOT NULL DEFAULT '0',
    decided_by         uuid REFERENCES accounts(id),
    decided_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_market_authz_agent_idx ON agent_market_authorizations (agent_account_id, created_at DESC);
CREATE INDEX agent_market_authz_pending_idx ON agent_market_authorizations (agent_account_id) WHERE decision = 'pending_approval';

-- Existing tables.
ALTER TABLE max_ids
    ADD COLUMN owner_changed_at timestamptz,
    ADD COLUMN listable_at timestamptz NOT NULL DEFAULT now();
UPDATE max_ids SET listable_at = created_at;

ALTER TABLE max_id_ownership_events ADD COLUMN sale_id uuid REFERENCES market_sales(id);

INSERT INTO reserved_names (canonical_name, claim_policy) VALUES ('market', 'blocked') ON CONFLICT DO NOTHING;

ALTER TABLE agent_policies ADD COLUMN market jsonb NOT NULL DEFAULT '{}'::jsonb;
