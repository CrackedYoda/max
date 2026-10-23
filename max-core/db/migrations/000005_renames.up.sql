-- Fee payments reuse the payments engine but pay MAX's treasury, not a MAX ID.
ALTER TABLE payments ALTER COLUMN max_id DROP NOT NULL;
ALTER TABLE payments ADD COLUMN purpose text NOT NULL DEFAULT 'transfer'
    CHECK (purpose IN ('transfer', 'rename_fee'));
ALTER TABLE payments ADD CONSTRAINT payments_transfer_has_max_id
    CHECK (purpose <> 'transfer' OR max_id IS NOT NULL);

CREATE TABLE rename_requests (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    max_id            uuid NOT NULL REFERENCES max_ids(id),
    owner_account_id  uuid NOT NULL REFERENCES accounts(id),
    old_name          text NOT NULL,
    new_name          text NOT NULL,
    status            text NOT NULL CHECK (status IN ('awaiting_payment', 'completed', 'failed', 'expired')),
    fee_payment_id    uuid NOT NULL UNIQUE REFERENCES payments(id),
    failure_reason    text NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CHECK (old_name <> new_name)
);
-- A pending request reserves its new name, and an ID has at most one pending request.
CREATE UNIQUE INDEX rename_requests_pending_name_uniq ON rename_requests (new_name) WHERE status = 'awaiting_payment';
CREATE UNIQUE INDEX rename_requests_pending_id_uniq ON rename_requests (max_id) WHERE status = 'awaiting_payment';

-- Previous names are held back so they can't be re-registered for phishing.
CREATE TABLE name_quarantine (
    canonical_name     text PRIMARY KEY,
    max_id             uuid NOT NULL REFERENCES max_ids(id),
    quarantined_until  timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now()
);
