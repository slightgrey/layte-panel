-- Per-site Redis object cache: {enabled, installed_plugin}.
ALTER TABLE sites ADD COLUMN object_cache_json TEXT NOT NULL DEFAULT '{}';
