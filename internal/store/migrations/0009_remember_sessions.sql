-- "Remember this browser for 30 days": such sessions use longer limits and
-- a persistent cookie (see api/session.go).
ALTER TABLE sessions ADD COLUMN remember INTEGER NOT NULL DEFAULT 0;
