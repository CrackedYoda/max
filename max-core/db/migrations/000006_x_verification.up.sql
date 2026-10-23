CREATE TABLE oauth_states (
    state_hash     bytea PRIMARY KEY,
    provider       text NOT NULL CHECK (provider IN ('x')),
    account_id     uuid NOT NULL REFERENCES accounts(id),
    -- The callback must arrive in this same browser session (OAuth CSRF defence).
    session_id     uuid NOT NULL REFERENCES sessions(id),
    code_verifier  text NOT NULL,
    expires_at     timestamptz NOT NULL,
    used_at        timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- Proof that an account controlled an external identity at verified_at.
-- Access tokens are never stored.
CREATE TABLE external_verifications (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id           uuid NOT NULL REFERENCES accounts(id),
    provider             text NOT NULL CHECK (provider IN ('x')),
    provider_user_id     text NOT NULL,
    username             text NOT NULL,
    display_name         text NOT NULL DEFAULT '',
    provider_created_at  timestamptz,
    verified_at          timestamptz NOT NULL,
    revoked_at           timestamptz,
    revoke_reason        text NOT NULL DEFAULT '',
    created_at           timestamptz NOT NULL DEFAULT now()
);
-- An external identity backs at most one MAX account at a time.
CREATE UNIQUE INDEX external_verifications_active_uniq
    ON external_verifications (provider, provider_user_id) WHERE revoked_at IS NULL;
CREATE INDEX external_verifications_account_idx ON external_verifications (account_id, verified_at DESC);

CREATE TABLE reserved_name_claims (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reserved_name_id  uuid NOT NULL REFERENCES reserved_names(id),
    canonical_name    text NOT NULL,
    account_id        uuid NOT NULL REFERENCES accounts(id),
    max_id            uuid REFERENCES max_ids(id),
    verification_id   uuid NOT NULL REFERENCES external_verifications(id),
    -- Pinned X identity: re-verification must come from this user id, even if the handle changes.
    x_user_id         text NOT NULL,
    x_username        text NOT NULL,
    status            text NOT NULL CHECK (status IN ('pending_review', 'active', 'lapsed', 'denied', 'revoked')),
    verified_until    timestamptz,
    review_note       text NOT NULL DEFAULT '',
    decided_by        uuid REFERENCES accounts(id),
    decided_at        timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
-- One live claim per reserved name.
CREATE UNIQUE INDEX reserved_name_claims_live_uniq
    ON reserved_name_claims (reserved_name_id) WHERE status IN ('pending_review', 'active', 'lapsed');
CREATE INDEX reserved_name_claims_account_idx ON reserved_name_claims (account_id);
CREATE INDEX reserved_name_claims_lapse_idx ON reserved_name_claims (verified_until) WHERE status = 'active';
