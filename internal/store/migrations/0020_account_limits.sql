-- Resource limits per site login (0 = none), and which sites run in their
-- login's own PHP-FPM master inside those limits.
ALTER TABLE users ADD COLUMN cpu_percent INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN memory_mb INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN disk_mb INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sites ADD COLUMN own_fpm INTEGER NOT NULL DEFAULT 0;
