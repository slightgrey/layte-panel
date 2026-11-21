-- Databases created by Layte Panel. Only these can be dropped or have their
-- password reset; others on the server are shown read-only.
CREATE TABLE databases (
    name       TEXT    PRIMARY KEY,
    db_user    TEXT    NOT NULL,
    site_id    INTEGER REFERENCES sites(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL
);

-- Databases of sites created before this table existed.
INSERT INTO databases (name, db_user, site_id, created_at)
SELECT db_name, db_user, id, created_at FROM sites WHERE managed = 1 AND db_name != '';
