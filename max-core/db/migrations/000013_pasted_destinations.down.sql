DELETE FROM max_id_destinations WHERE wallet_id IS NULL;
ALTER TABLE max_id_destinations
    DROP CONSTRAINT max_id_destinations_one_target,
    DROP COLUMN confirmed_at,
    DROP COLUMN address,
    ALTER COLUMN wallet_id SET NOT NULL;
