DROP INDEX IF EXISTS payments_awaiting_idx;
CREATE INDEX payments_awaiting_idx ON payments (expires_at) WHERE status = 'AWAITING_SENDER_SIGNATURE';
DROP INDEX IF EXISTS payments_due_idx;
CREATE INDEX payments_due_idx ON payments (next_check_at) WHERE status IN ('SUBMITTED', 'CONFIRMING');
DROP TRIGGER IF EXISTS payment_routes_quote_immutable ON payment_routes;
DROP FUNCTION IF EXISTS payment_routes_quote_immutable();
DROP TABLE IF EXISTS payment_routes;
DROP TABLE IF EXISTS settlement_preferences;
