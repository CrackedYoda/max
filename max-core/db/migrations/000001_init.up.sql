CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE accounts (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    type        text NOT NULL CHECK (type IN ('human', 'agent', 'organization')),
    status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE wallets (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id       uuid NOT NULL REFERENCES accounts(id),
    chain_namespace  text NOT NULL CHECK (chain_namespace IN ('evm', 'solana', 'bitcoin')),
    address          text NOT NULL,
    proof_message    text NOT NULL,
    proof_signature  text NOT NULL,
    status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    verified_at      timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);

-- An address can be actively linked to only one account at a time.
CREATE UNIQUE INDEX wallets_active_address_uniq
    ON wallets (chain_namespace, address) WHERE status = 'active';
CREATE INDEX wallets_account_idx ON wallets (account_id);

CREATE TABLE wallet_challenges (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    nonce            text NOT NULL UNIQUE,
    purpose          text NOT NULL CHECK (purpose IN ('login', 'link')),
    chain_namespace  text NOT NULL,
    address          text NOT NULL,
    account_id       uuid REFERENCES accounts(id),
    message          text NOT NULL,
    expires_at       timestamptz NOT NULL,
    used_at          timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id    uuid NOT NULL REFERENCES accounts(id),
    token_hash    bytea NOT NULL UNIQUE,
    ip            text,
    user_agent    text,
    expires_at    timestamptz NOT NULL,
    revoked_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_account_idx ON sessions (account_id);

CREATE TABLE max_ids (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    canonical_name       text NOT NULL UNIQUE,
    owner_account_id     uuid NOT NULL REFERENCES accounts(id),
    type                 text NOT NULL CHECK (type IN ('human', 'agent')),
    status               text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'locked', 'quarantined')),
    display_name         text NOT NULL DEFAULT '',
    description          text NOT NULL DEFAULT '',
    avatar_url           text NOT NULL DEFAULT '',
    -- Bumped whenever destinations change so payment preparation can reject stale resolutions.
    resolution_version   bigint NOT NULL DEFAULT 1,
    rename_available_at  timestamptz NOT NULL DEFAULT now(),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX max_ids_owner_idx ON max_ids (owner_account_id);

CREATE TABLE max_id_history (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    max_id          uuid NOT NULL REFERENCES max_ids(id),
    old_name        text NOT NULL,
    new_name        text NOT NULL,
    fee_payment_id  uuid,
    changed_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE max_id_ownership_events (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    max_id          uuid NOT NULL REFERENCES max_ids(id),
    previous_owner  uuid REFERENCES accounts(id),
    next_owner      uuid NOT NULL REFERENCES accounts(id),
    event_type      text NOT NULL CHECK (event_type IN ('claimed', 'transferred', 'revoked')),
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE reserved_names (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    canonical_name  text NOT NULL UNIQUE,
    claim_policy    text NOT NULL CHECK (claim_policy IN ('blocked', 'x_oauth', 'manual_review')),
    x_handle        text,
    status          text NOT NULL DEFAULT 'reserved' CHECK (status IN ('reserved', 'claimed', 'released')),
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE max_id_destinations (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    max_id           uuid NOT NULL REFERENCES max_ids(id),
    chain_namespace  text NOT NULL,
    wallet_id        uuid NOT NULL REFERENCES wallets(id),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (max_id, chain_namespace)
);
CREATE INDEX max_id_destinations_wallet_idx ON max_id_destinations (wallet_id);

CREATE TABLE audit_logs (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_account  uuid,
    action         text NOT NULL,
    resource_type  text NOT NULL,
    resource_id    text NOT NULL,
    metadata       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_resource_idx ON audit_logs (resource_type, resource_id);

INSERT INTO reserved_names (canonical_name, claim_policy) VALUES
    ('max', 'blocked'), ('admin', 'blocked'), ('root', 'blocked'), ('support', 'blocked'),
    ('help', 'blocked'), ('security', 'blocked'), ('official', 'blocked'), ('system', 'blocked'),
    ('api', 'blocked'), ('www', 'blocked'), ('wallet', 'blocked'), ('payments', 'blocked'),
    ('team', 'blocked'), ('staff', 'blocked'), ('null', 'blocked'), ('undefined', 'blocked');

INSERT INTO reserved_names (canonical_name, claim_policy, x_handle) VALUES
    ('solana', 'x_oauth', 'solana'), ('ethereum', 'x_oauth', 'ethereum'),
    ('base', 'x_oauth', 'base'), ('phantom', 'x_oauth', 'phantom'),
    ('metamask', 'x_oauth', 'MetaMask'), ('near', 'x_oauth', 'NEARProtocol'),
    ('coinbase', 'manual_review', 'coinbase'), ('binance', 'manual_review', 'binance');
