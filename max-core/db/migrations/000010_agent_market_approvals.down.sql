DROP INDEX IF EXISTS agent_market_authz_sale_idx;
DELETE FROM agent_market_authorizations WHERE action = 'pay';
ALTER TABLE agent_market_authorizations
    DROP COLUMN IF EXISTS finished_at,
    DROP COLUMN IF EXISTS failure,
    DROP COLUMN IF EXISTS payload,
    DROP CONSTRAINT agent_market_authorizations_action_check,
    ADD CONSTRAINT agent_market_authorizations_action_check CHECK (action IN ('buy', 'offer', 'list', 'accept'));
