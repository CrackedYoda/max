DELETE FROM max_id_destinations WHERE chain_key IS NOT NULL;
DROP INDEX max_id_destinations_chain_uniq;
DROP INDEX max_id_destinations_default_uniq;
ALTER TABLE max_id_destinations
    DROP CONSTRAINT max_id_destinations_override_evm,
    DROP COLUMN chain_key,
    ADD CONSTRAINT max_id_destinations_max_id_chain_namespace_key UNIQUE (max_id, chain_namespace);
