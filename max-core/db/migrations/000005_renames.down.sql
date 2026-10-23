DROP TABLE IF EXISTS name_quarantine;
DROP TABLE IF EXISTS rename_requests;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_transfer_has_max_id;
ALTER TABLE payments DROP COLUMN IF EXISTS purpose;
DELETE FROM payments WHERE max_id IS NULL;
ALTER TABLE payments ALTER COLUMN max_id SET NOT NULL;
