-- An agent is an account of type 'agent' administered by a human account.
-- Agents never hold sessions; they authenticate only with scoped credentials.
CREATE TABLE agents (
    account_id                uuid PRIMARY KEY REFERENCES accounts(id),
    administrator_account_id  uuid NOT NULL REFERENCES accounts(id),
    name                      text NOT NULL,
    description               text NOT NULL DEFAULT '',
    status                    text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
    created_at                timestamptz NOT NULL DEFAULT now(),
    updated_at                timestamptz NOT NULL DEFAULT now(),
    CHECK (account_id <> administrator_account_id)
);
CREATE INDEX agents_administrator_idx ON agents (administrator_account_id);

CREATE TABLE agent_policies (
    agent_account_id     uuid PRIMARY KEY REFERENCES agents(account_id),
    -- [{chain, asset, maxPerTransfer, maxPer24h}] in base units; assets not listed need approval.
    limits               jsonb NOT NULL DEFAULT '[]'::jsonb,
    allowed_recipients   text[] NOT NULL DEFAULT '{}',
    allow_any_recipient  boolean NOT NULL DEFAULT false,
    version              integer NOT NULL DEFAULT 1,
    updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE agent_credentials (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_account_id  uuid NOT NULL REFERENCES agents(account_id),
    name              text NOT NULL,
    prefix            text NOT NULL UNIQUE,
    secret_hash       bytea NOT NULL,
    scopes            text[] NOT NULL,
    created_by        uuid NOT NULL REFERENCES accounts(id),
    expires_at        timestamptz,
    last_used_at      timestamptz,
    revoked_at        timestamptz,
    replaced_by       uuid REFERENCES agent_credentials(id),
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_credentials_agent_idx ON agent_credentials (agent_account_id);

CREATE TABLE agent_transfer_authorizations (
    payment_id         uuid PRIMARY KEY REFERENCES payments(id),
    agent_account_id   uuid NOT NULL REFERENCES agents(account_id),
    credential_id      uuid NOT NULL REFERENCES agent_credentials(id),
    decision           text NOT NULL CHECK (decision IN ('auto_approved', 'pending_approval', 'approved', 'denied')),
    reasons            jsonb NOT NULL DEFAULT '[]'::jsonb,
    policy_version     integer NOT NULL,
    chain              text NOT NULL,
    asset_address      text NOT NULL,
    amount_base_units  text NOT NULL,
    -- The natural-language request, kept for audit only. It never authorizes anything.
    instruction        text NOT NULL DEFAULT '',
    decided_by         uuid REFERENCES accounts(id),
    decided_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_authz_agent_idx ON agent_transfer_authorizations (agent_account_id, created_at DESC);
CREATE INDEX agent_authz_pending_idx ON agent_transfer_authorizations (agent_account_id) WHERE decision = 'pending_approval';

DROP INDEX payments_awaiting_idx;
CREATE INDEX payments_awaiting_idx ON payments (expires_at) WHERE status IN ('CREATED', 'AWAITING_SENDER_SIGNATURE', 'QUOTED');
