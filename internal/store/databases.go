package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Database is a MySQL/MariaDB database created by Layte Panel.
type Database struct {
	Name      string    `json:"name"`
	User      string    `json:"user"`
	SiteID    int64     `json:"site_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) AddDatabase(ctx context.Context, d Database) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO databases (name, db_user, site_id, created_at) VALUES (?, ?, ?, ?)`,
		d.Name, d.User, nullID(d.SiteID), time.Now().Unix())
	if isUniqueViolation(err) {
		return fmt.Errorf("database %s: %w", d.Name, ErrExists)
	}
	return err
}

func (s *Store) RemoveDatabase(ctx context.Context, name string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM databases WHERE name=?`, name)
	return err
}

// UnlinkDatabase keeps the record but detaches it from its site.
func (s *Store) UnlinkDatabase(ctx context.Context, name string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE databases SET site_id=NULL WHERE name=?`, name)
	return err
}

func (s *Store) DatabaseByName(ctx context.Context, name string) (Database, error) {
	var d Database
	var site sql.NullInt64
	var created int64
	err := s.DB.QueryRowContext(ctx, `SELECT name, db_user, site_id, created_at FROM databases WHERE name=?`, name).
		Scan(&d.Name, &d.User, &site, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Database{}, ErrNotFound
	}
	if err != nil {
		return Database{}, err
	}
	d.SiteID, d.CreatedAt = site.Int64, time.Unix(created, 0)
	return d, nil
}

func (s *Store) ListDatabases(ctx context.Context) ([]Database, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT name, db_user, site_id, created_at FROM databases ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Database
	for rows.Next() {
		var d Database
		var site sql.NullInt64
		var created int64
		if err := rows.Scan(&d.Name, &d.User, &site, &created); err != nil {
			return nil, err
		}
		d.SiteID, d.CreatedAt = site.Int64, time.Unix(created, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}
