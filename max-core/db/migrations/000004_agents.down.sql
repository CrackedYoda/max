DROP INDEX IF EXISTS payments_awaiting_idx;
CREATE INDEX payments_awaiting_idx ON payments (expires_at) WHERE status IN ('AWAITING_SENDER_SIGNATURE', 'QUOTED');
DROP TABLE IF EXISTS agent_transfer_authorizations;
DROP TABLE IF EXISTS agent_credentials;
DROP TABLE IF EXISTS agent_policies;
DROP TABLE IF EXISTS agents;
