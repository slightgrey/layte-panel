-- Per-site redirects ([{from, to, code}]) rendered into the site's vhost.
ALTER TABLE sites ADD COLUMN redirects_json TEXT NOT NULL DEFAULT '[]';
