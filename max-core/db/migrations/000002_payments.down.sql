DROP TABLE IF EXISTS payment_events;
DROP TRIGGER IF EXISTS payments_snapshot_immutable ON payments;
DROP FUNCTION IF EXISTS payments_snapshot_immutable();
DROP TABLE IF EXISTS payments;
