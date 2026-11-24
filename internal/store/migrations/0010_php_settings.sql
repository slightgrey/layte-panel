-- Per-site PHP settings (simple mode: memory_limit, upload size, max
-- execution time, max input vars), as JSON. '{}' means Layte's defaults.
ALTER TABLE sites ADD COLUMN php_settings_json TEXT NOT NULL DEFAULT '{}';
