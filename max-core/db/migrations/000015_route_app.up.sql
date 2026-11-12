-- The web app's /app page (mobile app). Profiles live at /{handle}, so a MAX ID
-- with one of these names would be unreachable.
INSERT INTO reserved_names (canonical_name, claim_policy) VALUES
    ('app', 'blocked'), ('mobile', 'blocked')
ON CONFLICT (canonical_name) DO NOTHING;
