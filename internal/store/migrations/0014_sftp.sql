-- Per-site SFTP login: {enabled, password, keys: [{key, fingerprint}]}.
-- Public keys only; passwords are never stored.
ALTER TABLE sites ADD COLUMN sftp_json TEXT NOT NULL DEFAULT '{}';
