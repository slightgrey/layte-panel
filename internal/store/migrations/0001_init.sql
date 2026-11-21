CREATE TABLE users (
    id              INTEGER PRIMARY KEY,
    username        TEXT    NOT NULL UNIQUE,
    password_hash   TEXT    NOT NULL,
    role            TEXT    NOT NULL DEFAULT 'admin' CHECK (role IN ('admin')),
    totp_secret_enc BLOB,                       -- AES-256-GCM encrypted TOTP seed
    totp_last_step  INTEGER NOT NULL DEFAULT 0, -- last accepted TOTP time step (replay protection)
    failed_logins   INTEGER NOT NULL DEFAULT 0,
    locked_until    INTEGER NOT NULL DEFAULT 0, -- unix seconds
    created_at      INTEGER NOT NULL            -- unix seconds
);

CREATE TABLE sessions (
    token_hash   TEXT    PRIMARY KEY,           -- hex SHA-256 of the cookie token
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token   TEXT    NOT NULL,
    totp_pending INTEGER NOT NULL DEFAULT 1,    -- 1 until the second factor is passed
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    ip           TEXT    NOT NULL DEFAULT '',
    user_agent   TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_id ON sessions(user_id);

CREATE TABLE audit_log (
    id        INTEGER PRIMARY KEY,
    ts        INTEGER NOT NULL,
    user_id   INTEGER,
    username  TEXT    NOT NULL DEFAULT '',
    action    TEXT    NOT NULL,
    args_json TEXT    NOT NULL DEFAULT '{}',   -- secrets redacted before insert
    result    TEXT    NOT NULL CHECK (result IN ('ok', 'error')),
    error     TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_ts ON audit_log(ts);
