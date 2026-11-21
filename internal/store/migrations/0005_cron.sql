CREATE TABLE cron_jobs (
    id         INTEGER PRIMARY KEY,
    site_id    INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    schedule   TEXT    NOT NULL,
    command    TEXT    NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);
CREATE INDEX cron_jobs_site_id ON cron_jobs(site_id);
