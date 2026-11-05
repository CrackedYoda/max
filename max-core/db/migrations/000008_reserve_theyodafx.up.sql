-- Founder handle: claimable only by the @theyodafx X account.
INSERT INTO reserved_names (canonical_name, claim_policy, x_handle) VALUES
    ('theyodafx', 'x_oauth', 'theyodafx')
ON CONFLICT (canonical_name) DO NOTHING;
