-- name: CreateChallenge :one
INSERT INTO wallet_challenges (nonce, purpose, chain_namespace, address, account_id, message, expires_at, claim_name)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ConsumeChallenge :one
-- Single-use: the conditional UPDATE makes concurrent replays lose the race.
UPDATE wallet_challenges
SET used_at = now()
WHERE nonce = $1 AND used_at IS NULL AND expires_at > now()
RETURNING *;

-- name: GetActiveWalletByAddress :one
SELECT * FROM wallets
WHERE chain_namespace = $1 AND address = $2 AND status = 'active';

-- name: CreateAccount :one
INSERT INTO accounts (type) VALUES ($1) RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = $1;

-- name: LockAccount :exec
-- Serializes per-account invariants (last active wallet, MAX ID quota) that a
-- row lock on the changed row cannot protect under Read Committed. NO KEY
-- UPDATE still conflicts with itself but lets foreign-key inserts (sessions,
-- wallets, payments) that reference the account through.
SELECT id FROM accounts WHERE id = $1 FOR NO KEY UPDATE;

-- name: CreateWallet :one
INSERT INTO wallets (account_id, chain_namespace, address, proof_message, proof_signature, verified_at)
VALUES ($1, $2, $3, $4, $5, now())
RETURNING *;

-- name: CreateSession :one
INSERT INTO sessions (account_id, token_hash, ip, user_agent, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetActiveSessionByTokenHash :one
SELECT s.id, s.account_id, s.expires_at, a.type AS account_type, a.status AS account_status
FROM sessions s
JOIN accounts a ON a.id = s.account_id
WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now();

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;
