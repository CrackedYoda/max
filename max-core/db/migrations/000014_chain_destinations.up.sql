-- Per-chain overrides. An override row stores its EVM chain key in both
-- chain_key and chain_namespace, so the existing UNIQUE (max_id,
-- chain_namespace) keeps one row per default and per override, and binaries
-- that predate overrides keep working unchanged (they never read these rows).
ALTER TABLE max_id_destinations
    ADD COLUMN chain_key text,
    ADD CONSTRAINT max_id_destinations_override_key CHECK (
        chain_key IS NULL
        OR (chain_namespace = chain_key AND chain_key NOT IN ('evm', 'solana', 'bitcoin', 'litecoin', 'tron'))
    );
