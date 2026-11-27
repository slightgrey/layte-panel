// Package store is Layte Panel's own SQLite database (users, sessions, audit log).
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, no CGO)
)

// The `//go:embed` directive compiles the SQL files into the binary, so the
// migrations ship inside the single `layte` executable.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the database handle. Query methods live in other files of this package.
type Store struct {
	DB *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and runs migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate") // take the write lock at BEGIN, avoids SQLITE_BUSY upgrades
	dsn := "file:" + filepath.ToSlash(path) + "?" + q.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db %s: %w", path, err)
	}
	db.SetMaxOpenConns(4)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db %s: %w", path, err)
	}
	s := &Store{DB: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

// migrate applies every embedded migration not yet recorded in schema_migrations.
// Files are named NNNN_description.sql and applied in name order.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(filepath.Base(name), ".sql")
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&n); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if n > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile(name)
		if err != nil {
			return err
		}
		apply := s.applyMigration
		if strings.HasPrefix(string(body), fkOffMarker) {
			apply = s.applyMigrationFKOff
		}
		if err := apply(ctx, version, string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
	}
	return nil
}

// fkOffMarker as a migration's first line runs it with foreign keys off:
// SQLite's documented way to rebuild a table that other tables reference
// (otherwise DROP TABLE would fire their ON DELETE actions).
const fkOffMarker = "-- layte:foreign-keys-off"

func (s *Store) applyMigration(ctx context.Context, version, body string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, body); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version)
		return err
	})
}

// applyMigrationFKOff pins one connection (the PRAGMA is per connection and
// has no effect inside a transaction), turns foreign keys off, applies the
// migration in a transaction, checks the keys still hold, and turns them back on.
func (s *Store) applyMigrationFKOff(ctx context.Context, version, body string) error {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	bad := rows.Next()
	rows.Close()
	if bad {
		return errors.New("foreign key check failed")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
		return err
	}
	return tx.Commit()
}

// inTx runs fn inside a transaction, committing on success and rolling back on error.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
