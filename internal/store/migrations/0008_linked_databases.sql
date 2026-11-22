-- Existing databases linked to an adopted site. Layte didn't create them:
-- they can be exported and imported, but never dropped or have their
-- password reset (their app's config holds the password).
ALTER TABLE databases ADD COLUMN linked INTEGER NOT NULL DEFAULT 0;
