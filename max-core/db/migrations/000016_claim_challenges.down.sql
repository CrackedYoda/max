DELETE FROM wallet_challenges WHERE purpose = 'claim';
ALTER TABLE wallet_challenges DROP COLUMN claim_name;
ALTER TABLE wallet_challenges DROP CONSTRAINT wallet_challenges_purpose_check;
ALTER TABLE wallet_challenges ADD CONSTRAINT wallet_challenges_purpose_check CHECK (purpose IN ('login', 'link'));
