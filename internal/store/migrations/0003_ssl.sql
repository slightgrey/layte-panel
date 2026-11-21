-- Certificate file referenced by a site's nginx config (imported sites).
ALTER TABLE sites ADD COLUMN cert_path TEXT NOT NULL DEFAULT '';

-- Small key/value settings, e.g. the default Let's Encrypt account email.
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
