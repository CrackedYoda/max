-- Agent market approvals (spec §8): paying an accepted offer is policy-checked,
-- and a pending action keeps its full input so the administrator's approval
-- can replay it.
ALTER TABLE agent_market_authorizations
    DROP CONSTRAINT agent_market_authorizations_action_check,
    ADD CONSTRAINT agent_market_authorizations_action_check CHECK (action IN ('buy', 'offer', 'list', 'accept', 'pay')),
    ADD COLUMN payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- Set when an authorized action (or its replay) failed; such an
    -- authorization never counts toward the agent's spend.
    ADD COLUMN failure text,
    ADD COLUMN finished_at timestamptz;

CREATE INDEX agent_market_authz_sale_idx ON agent_market_authorizations (sale_id) WHERE sale_id IS NOT NULL;
