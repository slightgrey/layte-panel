-- layte:foreign-keys-off
-- Site accounts: a login tied to one site (role 'site'), plus subdomain sites
-- that belong to a main site and share its Linux user.
--
-- users is rebuilt because SQLite can't change a CHECK constraint in place.
-- The migrator runs this file with foreign keys off (see the marker above),
-- so dropping the old table doesn't cascade into sessions/api_keys, and runs
-- PRAGMA foreign_key_check before committing.

CREATE TABLE users_new (
    id              INTEGER PRIMARY KEY,
    username        TEXT    NOT NULL UNIQUE,
    password_hash   TEXT    NOT NULL,
    role            TEXT    NOT NULL DEFAULT 'admin' CHECK (role IN ('admin', 'site')),
    totp_secret_enc BLOB,                       -- AES-256-GCM encrypted TOTP seed; NULL = not set up yet
    totp_last_step  INTEGER NOT NULL DEFAULT 0,
    failed_logins   INTEGER NOT NULL DEFAULT 0,
    locked_until    INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    site_id         INTEGER UNIQUE REFERENCES sites(id) ON DELETE CASCADE, -- main site of a site account
    max_subdomains  INTEGER NOT NULL DEFAULT 5,
    CHECK ((role = 'site') = (site_id IS NOT NULL))
);
INSERT INTO users_new (id, username, password_hash, role, totp_secret_enc, totp_last_step, failed_logins, locked_until, created_at)
    SELECT id, username, password_hash, role, totp_secret_enc, totp_last_step, failed_logins, locked_until, created_at FROM users;
DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

-- A subdomain site belongs to a main site and uses its Linux user.
ALTER TABLE sites ADD COLUMN parent_site_id INTEGER REFERENCES sites(id);
CREATE INDEX sites_parent ON sites(parent_site_id);

-- Browser 2FA setup: the not-yet-confirmed secret, encrypted, on the pending session.
ALTER TABLE sessions ADD COLUMN totp_setup_enc BLOB;
