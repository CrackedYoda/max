DELETE FROM reserved_names
WHERE canonical_name IN ('app', 'mobile')
  AND claim_policy = 'blocked';
