-- Default MAX IDs (spec §3): every account gets one long random ID it can
-- receive on without the registration gate.
ALTER TABLE max_ids ADD COLUMN is_default boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX max_ids_one_default ON max_ids (owner_account_id) WHERE is_default;
