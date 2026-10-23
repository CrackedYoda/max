-- Recipient's opt-in conversion target, used when a sender pays on a chain
-- the recipient has no wallet for.
CREATE TABLE settlement_preferences (
    max_id             uuid PRIMARY KEY REFERENCES max_ids(id),
    destination_chain  text NOT NULL,
    destination_asset  text NOT NULL DEFAULT '',  -- '' = native coin
    enabled            boolean NOT NULL DEFAULT true,
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE payment_routes (
    payment_id                  uuid PRIMARY KEY REFERENCES payments(id),
    provider                    text NOT NULL,
    swap_type                   text NOT NULL CHECK (swap_type IN ('EXACT_INPUT', 'EXACT_OUTPUT')),
    -- What the sender asked for: input base units (EXACT_INPUT) or output base units (EXACT_OUTPUT).
    requested_amount            text NOT NULL,
    origin_asset_id             text NOT NULL,
    destination_asset_id        text NOT NULL,
    destination_chain           text NOT NULL,
    destination_asset_kind      text NOT NULL CHECK (destination_asset_kind IN ('native', 'token')),
    destination_asset_address   text NOT NULL DEFAULT '',
    destination_asset_decimals  integer NOT NULL,
    destination_asset_symbol    text NOT NULL DEFAULT '',
    deposit_address             text NOT NULL UNIQUE,
    refund_to                   text NOT NULL,
    amount_in                   text NOT NULL,
    amount_out_expected         text NOT NULL,
    min_amount_out              text NOT NULL,
    amount_in_usd               text NOT NULL DEFAULT '',
    amount_out_usd              text NOT NULL DEFAULT '',
    slippage_bps                integer NOT NULL CHECK (slippage_bps BETWEEN 1 AND 1000),
    time_estimate_seconds       integer NOT NULL DEFAULT 0,
    quote_deadline              timestamptz NOT NULL,
    quote_signature             text NOT NULL,
    correlation_id              text NOT NULL DEFAULT '',
    -- Full provider response, kept for dispute resolution as 1Click requires.
    quote_raw                   jsonb NOT NULL,

    deposit_confirmed_at        timestamptz,
    provider_status             text NOT NULL DEFAULT '',
    provider_updated_at         timestamptz,
    amount_out_actual           text,
    refunded_amount             text,
    refund_reason               text,
    destination_tx_hashes       jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION payment_routes_quote_immutable() RETURNS trigger AS $$
BEGIN
    IF NEW.provider              IS DISTINCT FROM OLD.provider
    OR NEW.swap_type             IS DISTINCT FROM OLD.swap_type
    OR NEW.requested_amount      IS DISTINCT FROM OLD.requested_amount
    OR NEW.origin_asset_id       IS DISTINCT FROM OLD.origin_asset_id
    OR NEW.destination_asset_id  IS DISTINCT FROM OLD.destination_asset_id
    OR NEW.destination_chain     IS DISTINCT FROM OLD.destination_chain
    OR NEW.deposit_address       IS DISTINCT FROM OLD.deposit_address
    OR NEW.refund_to             IS DISTINCT FROM OLD.refund_to
    OR NEW.amount_in             IS DISTINCT FROM OLD.amount_in
    OR NEW.min_amount_out        IS DISTINCT FROM OLD.min_amount_out
    OR NEW.quote_deadline        IS DISTINCT FROM OLD.quote_deadline
    OR NEW.quote_signature       IS DISTINCT FROM OLD.quote_signature
    OR NEW.quote_raw             IS DISTINCT FROM OLD.quote_raw THEN
        RAISE EXCEPTION 'payment route quote is immutable' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER payment_routes_quote_immutable
    BEFORE UPDATE ON payment_routes
    FOR EACH ROW EXECUTE FUNCTION payment_routes_quote_immutable();

DROP INDEX payments_due_idx;
CREATE INDEX payments_due_idx ON payments (next_check_at) WHERE status IN ('SUBMITTED', 'CONFIRMING', 'REFUND_PENDING');
DROP INDEX payments_awaiting_idx;
CREATE INDEX payments_awaiting_idx ON payments (expires_at) WHERE status IN ('AWAITING_SENDER_SIGNATURE', 'QUOTED');
