-- Adopted sites whose nginx vhost (and PHP pool) Layte Panel took over:
-- rendered from Layte's templates, with the site's own web root.
ALTER TABLE sites ADD COLUMN nginx_managed INTEGER NOT NULL DEFAULT 0;
