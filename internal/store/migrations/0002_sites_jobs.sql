CREATE TABLE sites (
    id           INTEGER PRIMARY KEY,
    domain       TEXT    NOT NULL UNIQUE,         -- ASCII (punycode) form
    aliases_json TEXT    NOT NULL DEFAULT '[]',
    system_user  TEXT    NOT NULL DEFAULT '',
    root_path    TEXT    NOT NULL DEFAULT '',     -- web root
    php_version  TEXT    NOT NULL DEFAULT '',
    managed      INTEGER NOT NULL DEFAULT 0,      -- 1 = created by Layte, 0 = imported (read-only)
    source       TEXT    NOT NULL CHECK (source IN ('created', 'imported')),
    wordpress    INTEGER NOT NULL DEFAULT 0,
    ssl          INTEGER NOT NULL DEFAULT 0,
    db_name      TEXT    NOT NULL DEFAULT '',
    db_user      TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'active' CHECK (status IN ('creating', 'active', 'failed', 'deleting')),
    nginx_conf   TEXT    NOT NULL DEFAULT '',     -- path of the vhost file
    fpm_pool     TEXT    NOT NULL DEFAULT '',     -- path of the pool file (managed sites)
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX sites_system_user ON sites(system_user);

CREATE TABLE jobs (
    id          TEXT    PRIMARY KEY,
    kind        TEXT    NOT NULL,                 -- site.create, site.delete, site.php_switch
    site_id     INTEGER,                          -- no FK: the site row may be deleted by the job
    user_id     INTEGER,
    status      TEXT    NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    events_json TEXT    NOT NULL DEFAULT '[]',
    error       TEXT    NOT NULL DEFAULT '',
    result_json TEXT    NOT NULL DEFAULT 'null',  -- never contains secrets
    created_at  INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX jobs_created_at ON jobs(created_at);
