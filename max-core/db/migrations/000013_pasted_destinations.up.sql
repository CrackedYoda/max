-- A destination points at either a verified wallet or a pasted, owner-confirmed
-- address. Pasted addresses never become wallets.
ALTER TABLE max_id_destinations
    ALTER COLUMN wallet_id DROP NOT NULL,
    ADD COLUMN address text,
    ADD COLUMN confirmed_at timestamptz,
    ADD CONSTRAINT max_id_destinations_one_target CHECK (
        (wallet_id IS NOT NULL AND address IS NULL AND confirmed_at IS NULL)
        OR (wallet_id IS NULL AND address IS NOT NULL AND confirmed_at IS NOT NULL)
    );
