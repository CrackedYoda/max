-- Rows are kept: default IDs become ordinary IDs.
DROP INDEX IF EXISTS max_ids_one_default;
ALTER TABLE max_ids DROP COLUMN IF EXISTS is_default;
