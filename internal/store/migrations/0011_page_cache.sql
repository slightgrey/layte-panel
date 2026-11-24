-- Per-site page cache (nginx FastCGI cache) settings, as JSON. '{}' = off.
ALTER TABLE sites ADD COLUMN page_cache_json TEXT NOT NULL DEFAULT '{}';
