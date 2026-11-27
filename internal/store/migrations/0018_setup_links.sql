-- One-time setup links: a new site login (or a password reset) gets a link
-- instead of a generated password. Only the token's SHA-256 is stored.
ALTER TABLE users ADD COLUMN setup_token_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN setup_expires INTEGER NOT NULL DEFAULT 0;
