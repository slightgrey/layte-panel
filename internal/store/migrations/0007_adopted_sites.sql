-- Adopted sites: imported sites (managed = 0, their nginx config stays the
-- admin's) that Layte now runs under its own Linux user (system_user) with a
-- Layte-managed PHP-FPM pool (fpm_pool). Scans keep both.
ALTER TABLE sites ADD COLUMN adopted INTEGER NOT NULL DEFAULT 0;
