DELETE FROM max_id_destinations WHERE chain_key IS NOT NULL;
ALTER TABLE max_id_destinations
    DROP CONSTRAINT max_id_destinations_override_key,
    DROP COLUMN chain_key;
