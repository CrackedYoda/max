CREATE TABLE payments (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_account_id       uuid REFERENCES accounts(id),
    sender_address          text NOT NULL,
    idempotency_key         text NOT NULL,

    -- Recipient resolution snapshot, locked at prepare time.
    max_id                  uuid NOT NULL REFERENCES max_ids(id),
    recipient_handle        text NOT NULL,
    recipient_address       text NOT NULL,
    resolution_version      bigint NOT NULL,

    chain                   text NOT NULL,
    chain_namespace         text NOT NULL CHECK (chain_namespace IN ('evm', 'solana')),
    asset_kind              text NOT NULL CHECK (asset_kind IN ('native', 'token')),
    asset_address           text NOT NULL DEFAULT '',
    asset_decimals          integer NOT NULL CHECK (asset_decimals BETWEEN 0 AND 36),
    asset_symbol            text NOT NULL DEFAULT '',
    asset_verified          boolean NOT NULL DEFAULT false,
    token_program           text NOT NULL DEFAULT '',
    amount_base_units       text NOT NULL CHECK (amount_base_units ~ '^[1-9][0-9]{0,77}$'),
    route_type              text NOT NULL DEFAULT 'direct' CHECK (route_type IN ('direct', 'cross_chain')),

    status                  text NOT NULL CHECK (status IN (
                                'CREATED', 'QUOTED', 'AWAITING_SENDER_SIGNATURE', 'SUBMITTED', 'CONFIRMING',
                                'COMPLETED', 'EXPIRED', 'FAILED', 'REFUND_PENDING', 'REFUNDED', 'MANUAL_REVIEW')),
    status_reason           text NOT NULL DEFAULT '',
    tx_hash                 text,
    confirmations           bigint NOT NULL DEFAULT 0,
    required_confirmations  bigint NOT NULL,

    expires_at              timestamptz NOT NULL,
    submitted_at            timestamptz,
    completed_at            timestamptz,
    next_check_at           timestamptz,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),

    UNIQUE (sender_address, idempotency_key)
);

-- One on-chain transaction can settle at most one payment.
CREATE UNIQUE INDEX payments_chain_tx_uniq ON payments (chain, tx_hash) WHERE tx_hash IS NOT NULL;
CREATE INDEX payments_due_idx ON payments (next_check_at) WHERE status IN ('SUBMITTED', 'CONFIRMING');
CREATE INDEX payments_awaiting_idx ON payments (expires_at) WHERE status = 'AWAITING_SENDER_SIGNATURE';
CREATE INDEX payments_sender_account_idx ON payments (sender_account_id, created_at DESC);
CREATE INDEX payments_max_id_idx ON payments (max_id, created_at DESC);

-- The snapshot is the authorization record: once written it must never change,
-- even if application code has a bug.
CREATE FUNCTION payments_snapshot_immutable() RETURNS trigger AS $$
BEGIN
    IF NEW.sender_address     IS DISTINCT FROM OLD.sender_address
    OR NEW.max_id             IS DISTINCT FROM OLD.max_id
    OR NEW.recipient_address  IS DISTINCT FROM OLD.recipient_address
    OR NEW.resolution_version IS DISTINCT FROM OLD.resolution_version
    OR NEW.chain              IS DISTINCT FROM OLD.chain
    OR NEW.asset_kind         IS DISTINCT FROM OLD.asset_kind
    OR NEW.asset_address      IS DISTINCT FROM OLD.asset_address
    OR NEW.asset_decimals     IS DISTINCT FROM OLD.asset_decimals
    OR NEW.amount_base_units  IS DISTINCT FROM OLD.amount_base_units
    OR (OLD.tx_hash IS NOT NULL AND NEW.tx_hash IS DISTINCT FROM OLD.tx_hash) THEN
        RAISE EXCEPTION 'payment snapshot is immutable' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER payments_snapshot_immutable
    BEFORE UPDATE ON payments
    FOR EACH ROW EXECUTE FUNCTION payments_snapshot_immutable();

CREATE TABLE payment_events (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id   uuid NOT NULL REFERENCES payments(id),
    event_type   text NOT NULL,
    from_status  text,
    to_status    text NOT NULL,
    detail       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payment_events_payment_idx ON payment_events (payment_id, created_at);
