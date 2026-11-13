-- Claiming a name needs a signed message from one of the account's wallets.
-- The challenge records which name it is for.
ALTER TABLE wallet_challenges DROP CONSTRAINT wallet_challenges_purpose_check;
ALTER TABLE wallet_challenges ADD CONSTRAINT wallet_challenges_purpose_check CHECK (purpose IN ('login', 'link', 'claim'));
ALTER TABLE wallet_challenges ADD COLUMN claim_name text;
