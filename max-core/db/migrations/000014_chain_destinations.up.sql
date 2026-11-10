-- Per-chain overrides: a NULL chain_key row is the namespace default; a row
-- with a chain_key overrides that one EVM chain.
ALTER TABLE max_id_destinations
    ADD COLUMN chain_key text,
    DROP CONSTRAINT max_id_destinations_max_id_chain_namespace_key,
    ADD CONSTRAINT max_id_destinations_override_evm CHECK (chain_key IS NULL OR chain_namespace = 'evm');
CREATE UNIQUE INDEX max_id_destinations_default_uniq
    ON max_id_destinations (max_id, chain_namespace) WHERE chain_key IS NULL;
CREATE UNIQUE INDEX max_id_destinations_chain_uniq
    ON max_id_destinations (max_id, chain_key) WHERE chain_key IS NOT NULL;
