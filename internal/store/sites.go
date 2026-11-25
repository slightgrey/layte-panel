package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/slightgrey/layte-panel/internal/pagecache"
	"github.com/slightgrey/layte-panel/internal/phpini"
)

type Site struct {
	ID         int64    `json:"id"`
	Domain     string   `json:"domain"`
	Aliases    []string `json:"aliases"`
	SystemUser string   `json:"system_user"`
	RootPath   string   `json:"root_path"`
	PHPVersion string   `json:"php_version"`
	Managed    bool     `json:"managed"`
	Source     string   `json:"source"`  // created | imported
	Adopted    bool     `json:"adopted"` // imported, then adopted: runs as SystemUser with a Layte pool
	// NginxManaged: an adopted site whose vhost and pool Layte took over
	// (rendered from its templates, keeping the site's own web root).
	NginxManaged bool   `json:"nginx_managed"`
	WordPress    bool   `json:"wordpress"`
	SSL          bool   `json:"ssl"`
	DBName       string `json:"db_name"`
	DBUser       string `json:"db_user"`
	Status       string `json:"status"`
	NginxConf    string `json:"nginx_conf"`
	FPMPool      string `json:"fpm_pool"`
	CertPath     string `json:"cert_path"`
	// PHPSettings are the site's PHP settings (managed sites; zero = defaults).
	PHPSettings phpini.Settings `json:"php_settings"`
	// PageCache is the site's page cache (managed WordPress sites).
	PageCache pagecache.Settings `json:"page_cache"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

const siteCols = `id, domain, aliases_json, system_user, root_path, php_version, managed, source, wordpress, ssl,
	db_name, db_user, status, nginx_conf, fpm_pool, cert_path, adopted, php_settings_json, page_cache_json, nginx_managed, created_at, updated_at`

func scanSite(row interface{ Scan(...any) error }) (Site, error) {
	var s Site
	var aliases, php, cache string
	var created, updated int64
	err := row.Scan(&s.ID, &s.Domain, &aliases, &s.SystemUser, &s.RootPath, &s.PHPVersion, &s.Managed, &s.Source,
		&s.WordPress, &s.SSL, &s.DBName, &s.DBUser, &s.Status, &s.NginxConf, &s.FPMPool, &s.CertPath, &s.Adopted, &php, &cache, &s.NginxManaged, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, ErrNotFound
	}
	if err != nil {
		return Site{}, err
	}
	if err := json.Unmarshal([]byte(aliases), &s.Aliases); err != nil || s.Aliases == nil {
		s.Aliases = []string{}
	}
	json.Unmarshal([]byte(php), &s.PHPSettings) // bad JSON: defaults
	json.Unmarshal([]byte(cache), &s.PageCache)
	s.CreatedAt, s.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	return s, nil
}

// LayteConfigs reports whether Layte renders the site's nginx vhost and PHP
// pool from its templates: sites it created, and adopted sites it took over.
// PHP switching, PHP settings, the page cache and Layte certificates need it.
func (s Site) LayteConfigs() bool { return s.Managed || (s.Adopted && s.NginxManaged) }

func phpSettingsJSON(p phpini.Settings) string { return toJSON(p) }

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
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
		(domain, aliases_json, system_user, root_path, php_version, managed, source, wordpress, ssl, db_name, db_user, status, nginx_conf, fpm_pool, cert_path, php_settings_json, page_cache_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		x.Domain, aliasesJSON(x.Aliases), x.SystemUser, x.RootPath, x.PHPVersion, x.Managed, x.Source, x.WordPress, x.SSL,
		x.DBName, x.DBUser, x.Status, x.NginxConf, x.FPMPool, x.CertPath, phpSettingsJSON(x.PHPSettings), toJSON(x.PageCache), now, now)
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
		wordpress=?, ssl=?, db_name=?, db_user=?, status=?, nginx_conf=?, fpm_pool=?, cert_path=?, adopted=?, php_settings_json=?, page_cache_json=?, nginx_managed=?, updated_at=? WHERE id=?`,
		aliasesJSON(x.Aliases), x.SystemUser, x.RootPath, x.PHPVersion, x.Managed, x.WordPress, x.SSL,
		x.DBName, x.DBUser, x.Status, x.NginxConf, x.FPMPool, x.CertPath, x.Adopted, phpSettingsJSON(x.PHPSettings), toJSON(x.PageCache), x.NginxManaged, time.Now().Unix(), x.ID)
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
			var managed, adopted bool
			err := tx.QueryRowContext(ctx, `SELECT managed, adopted FROM sites WHERE domain=?`, x.Domain).Scan(&managed, &adopted)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				_, err = tx.ExecContext(ctx, `INSERT INTO sites
					(domain, aliases_json, system_user, root_path, php_version, managed, source, wordpress, ssl, status, nginx_conf, cert_path, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, 0, 'imported', ?, ?, 'active', ?, ?, ?, ?)`,
					x.Domain, aliasesJSON(x.Aliases), x.SystemUser, x.RootPath, x.PHPVersion, x.WordPress, x.SSL, x.NginxConf, x.CertPath, now, now)
				if err != nil {
					return err
				}
				added++
			case err != nil:
				return err
			case adopted:
				// The vhost is still the admin's, so follow it; the user, pool and
				// PHP version are Layte's now.
				_, err = tx.ExecContext(ctx, `UPDATE sites SET aliases_json=?, root_path=?, wordpress=?, ssl=?, nginx_conf=?, cert_path=?, updated_at=?
					WHERE domain=? AND adopted=1`,
					aliasesJSON(x.Aliases), x.RootPath, x.WordPress, x.SSL, x.NginxConf, x.CertPath, now, x.Domain)
				if err != nil {
					return err
				}
			case !managed:
				_, err = tx.ExecContext(ctx, `UPDATE sites SET aliases_json=?, system_user=?, root_path=?, php_version=?, wordpress=?, ssl=?, nginx_conf=?, cert_path=?, updated_at=?
					WHERE domain=? AND managed=0`,
					aliasesJSON(x.Aliases), x.SystemUser, x.RootPath, x.PHPVersion, x.WordPress, x.SSL, x.NginxConf, x.CertPath, now, x.Domain)
				if err != nil {
					return err
				}
			}
		}
		// Adopted sites are never dropped by a scan: they own a Linux user and a
		// pool, so removing them is a deliberate action.
		rows, err := tx.QueryContext(ctx, `SELECT id, domain FROM sites WHERE managed=0 AND adopted=0`)
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
