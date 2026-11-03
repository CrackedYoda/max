DELETE FROM reserved_names
WHERE canonical_name IN ('agents', 'agent', 'verify', 'rename', 'dashboard', 'send', 'claim', 'connect')
  AND claim_policy = 'blocked';
