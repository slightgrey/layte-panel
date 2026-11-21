package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Site struct {
	ID         int64     `json:"id"`
	Domain     string    `json:"domain"`
	Aliases    []string  `json:"aliases"`
	SystemUser string    `json:"system_user"`
	RootPath   string    `json:"root_path"`
	PHPVersion string    `json:"php_version"`
	Managed    bool      `json:"managed"`
	Source     string    `json:"source"` // created | imported
	WordPress  bool      `json:"wordpress"`
	SSL        bool      `json:"ssl"`
	DBName     string    `json:"db_name"`
	DBUser     string    `json:"db_user"`
	Status     string    `json:"status"`
	NginxConf  string    `json:"nginx_conf"`
	FPMPool    string    `json:"fpm_pool"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const siteCols = `id, domain, aliases_json, system_user, root_path, php_version, managed, source, wordpress, ssl,
	db_name, db_user, status, nginx_conf, fpm_pool, created_at, updated_at`

func scanSite(row interface{ Scan(...any) error }) (Site, error) {
	var s Site
	var aliases string
	var created, updated int64
	err := row.Scan(&s.ID, &s.Domain, &aliases, &s.SystemUser, &s.RootPath, &s.PHPVersion, &s.Managed, &s.Source,
		&s.WordPress, &s.SSL, &s.DBName, &s.DBUser, &s.Status, &s.NginxConf, &s.FPMPool, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, ErrNotFound
	}
	if err != nil {
		return Site{}, err
	}
	if err := json.Unmarshal([]byte(aliases), &s.Aliases); err != nil || s.Aliases == nil {
		s.Aliases = []string{}
	}
	s.CreatedAt, s.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	return s, nil
}

func aliasesJSON(a []string) string {
	if a == nil {
		a = []string{}
	}
	b, _ := json.Marshal(a)
	return string(b)
}

// CreateSite inserts a site and returns its id.
func (s *Store) CreateSite(ctx context.Context, x Site) (int64, error) {
	now := time.Now().Unix()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO sites
		(domain, aliases_json, system_user, root_path, php_version, managed, source, wordpress, ssl, db_name, db_user, status, nginx_conf, fpm_pool, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		x.Domain, aliasesJSON(x.Aliases), x.SystemUser, x.RootPath, x.PHPVersion, x.Managed, x.Source, x.WordPress, x.SSL,
		x.DBName, x.DBUser, x.Status, x.NginxConf, x.FPMPool, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("site %s: %w", x.Domain, ErrExists)
		}
		return 0, fmt.Errorf("create site %s: %w", x.Domain, err)
	}
	return res.LastInsertId()
}

// UpdateSite saves every mutable column of x (matched by id).
func (s *Store) UpdateSite(ctx context.Context, x Site) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sites SET aliases_json=?, system_user=?, root_path=?, php_version=?, managed=?,
		wordpress=?, ssl=?, db_name=?, db_user=?, status=?, nginx_conf=?, fpm_pool=?, updated_at=? WHERE id=?`,
		aliasesJSON(x.Aliases), x.SystemUser, x.RootPath, x.PHPVersion, x.Managed, x.WordPress, x.SSL,
		x.DBName, x.DBUser, x.Status, x.NginxConf, x.FPMPool, time.Now().Unix(), x.ID)
	if err != nil {
		return fmt.Errorf("update site %s: %w", x.Domain, err)
	}
	return nil
}

func (s *Store) SetSiteStatus(ctx context.Context, id int64, status string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sites SET status=?, updated_at=? WHERE id=?`, status, time.Now().Unix(), id)
	return err
}

func (s *Store) DeleteSite(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sites WHERE id=?`, id)
	return err
}

func (s *Store) SiteByID(ctx context.Context, id int64) (Site, error) {
	return scanSite(s.DB.QueryRowContext(ctx, `SELECT `+siteCols+` FROM sites WHERE id=?`, id))
}

func (s *Store) SiteByDomain(ctx context.Context, domain string) (Site, error) {
	return scanSite(s.DB.QueryRowContext(ctx, `SELECT `+siteCols+` FROM sites WHERE domain=?`, domain))
}

// ListSites returns all sites ordered by domain.
func (s *Store) ListSites(ctx context.Context) ([]Site, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+siteCols+` FROM sites ORDER BY domain`)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	defer rows.Close()
	out := []Site{}
	for rows.Next() {
		x, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SystemUserInUse reports whether any site uses the Linux user name.
func (s *Store) SystemUserInUse(ctx context.Context, user string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sites WHERE system_user=?`, user).Scan(&n)
	return n > 0, err
}

// ReplaceImported syncs imported (unmanaged) sites with a fresh scan: rows
// are upserted by domain and imported rows no longer found are removed.
// Managed sites are never touched; a scanned domain that matches a managed
// site is skipped.
func (s *Store) ReplaceImported(ctx context.Context, found []Site) (added, removed int, err error) {
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		seen := map[string]bool{}
		now := time.Now().Unix()
		for _, x := range found {
			seen[x.Domain] = true
			var managed bool
			err := tx.QueryRowContext(ctx, `SELECT managed FROM sites WHERE domain=?`, x.Domain).Scan(&managed)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				_, err = tx.ExecContext(ctx, `INSERT INTO sites
					(domain, aliases_json, system_user, root_path, php_version, managed, source, wordpress, ssl, status, nginx_conf, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, 0, 'imported', ?, ?, 'active', ?, ?, ?)`,
					x.Domain, aliasesJSON(x.Aliases), x.SystemUser, x.RootPath, x.PHPVersion, x.WordPress, x.SSL, x.NginxConf, now, now)
				if err != nil {
					return err
				}
				added++
			case err != nil:
				return err
			case !managed:
				_, err = tx.ExecContext(ctx, `UPDATE sites SET aliases_json=?, system_user=?, root_path=?, php_version=?, wordpress=?, ssl=?, nginx_conf=?, updated_at=?
					WHERE domain=? AND managed=0`,
					aliasesJSON(x.Aliases), x.SystemUser, x.RootPath, x.PHPVersion, x.WordPress, x.SSL, x.NginxConf, now, x.Domain)
				if err != nil {
					return err
				}
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, domain FROM sites WHERE managed=0`)
		if err != nil {
			return err
		}
		var gone []int64
		for rows.Next() {
			var id int64
			var d string
			if err := rows.Scan(&id, &d); err != nil {
				rows.Close()
				return err
			}
			if !seen[d] {
				gone = append(gone, id)
			}
		}
		rows.Close()
		for _, id := range gone {
			if _, err := tx.ExecContext(ctx, `DELETE FROM sites WHERE id=?`, id); err != nil {
				return err
			}
		}
		removed = len(gone)
		return nil
	})
	return added, removed, err
}
