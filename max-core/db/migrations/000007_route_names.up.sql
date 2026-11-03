-- Top-level paths in the web app. Profiles live at /{handle}, so a MAX ID with
-- one of these names would be unreachable (the static route wins).
INSERT INTO reserved_names (canonical_name, claim_policy) VALUES
    ('agents', 'blocked'), ('agent', 'blocked'), ('verify', 'blocked'), ('rename', 'blocked'),
    ('dashboard', 'blocked'), ('send', 'blocked'), ('claim', 'blocked'), ('connect', 'blocked')
ON CONFLICT (canonical_name) DO NOTHING;
