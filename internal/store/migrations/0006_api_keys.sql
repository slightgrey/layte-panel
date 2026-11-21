CREATE TABLE api_keys (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    prefix       TEXT    NOT NULL,             -- first characters of the token, for display only
    token_hash   TEXT    NOT NULL UNIQUE,      -- hex SHA-256 of the token; the token itself is never stored
    scopes_json  TEXT    NOT NULL,
    created_by   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL DEFAULT 0,
    expires_at   INTEGER NOT NULL DEFAULT 0,   -- 0 = never
    revoked_at   INTEGER NOT NULL DEFAULT 0
);
