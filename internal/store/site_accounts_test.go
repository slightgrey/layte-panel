package store

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestSiteAccountsMigrationKeepsData upgrades a database that has an admin
// with a session and an API key: the users rebuild in 0016 must keep them.
func TestSiteAccountsMigrationKeepsData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	old := &Store{DB: db}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	names, _ := fs.Glob(migrationsFS, "migrations/*.sql")
	sort.Strings(names)
	for _, n := range names {
		v := strings.TrimSuffix(filepath.Base(n), ".sql")
		if v >= "0016" {
			break
		}
		body, _ := migrationsFS.ReadFile(n)
		if err := old.applyMigration(ctx, v, string(body)); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	now := time.Now().Unix()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO users (id, username, password_hash, totp_secret_enc, created_at) VALUES (7, 'alice', 'h', x'01', ?)`, now)
	mustExec(`INSERT INTO sessions (token_hash, user_id, csrf_token, totp_pending, created_at, last_seen_at) VALUES ('t', 7, 'c', 0, ?, ?)`, now, now)
	mustExec(`INSERT INTO api_keys (name, prefix, token_hash, scopes_json, created_by, created_at) VALUES ('k', 'p', 'kh', '[]', 7, ?)`, now)
	db.Close()

	s, err := Open(ctx, path) // applies 0016
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.UserByID(ctx, 7)
	if err != nil || u.Username != "alice" || !u.IsAdmin() || u.SiteID != 0 {
		t.Fatalf("admin after migration = %+v, %v", u, err)
	}
	if _, err := s.SessionByHash(ctx, "t"); err != nil {
		t.Errorf("session lost: %v", err)
	}
	var by sql.NullInt64
	if err := s.DB.QueryRow(`SELECT created_by FROM api_keys WHERE name='k'`).Scan(&by); err != nil || by.Int64 != 7 {
		t.Errorf("api key created_by = %v, %v", by, err)
	}
	var on int
	s.DB.QueryRow(`PRAGMA foreign_keys`).Scan(&on)
	if on != 1 {
		t.Errorf("foreign_keys = %d after migration", on)
	}
}

func TestSiteAccounts(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	main, err := s.CreateSite(ctx, Site{Domain: "example.com", SystemUser: "example", Managed: true, Source: "created", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.CreateSite(ctx, Site{Domain: "blog.example.com", SystemUser: "example", Managed: true, Source: "created", Status: "active", ParentSiteID: main})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSite(ctx, Site{Domain: "other.com", SystemUser: "other", Managed: true, Source: "created", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.SiteByID(ctx, sub); got.ParentSiteID != main {
		t.Errorf("ParentSiteID = %d, want %d", got.ParentSiteID, main)
	}
	sites, err := s.SitesForAccount(ctx, main)
	if err != nil || len(sites) != 2 {
		t.Fatalf("SitesForAccount = %d sites, %v", len(sites), err)
	}
	if n, _ := s.CountSubdomains(ctx, main); n != 1 {
		t.Errorf("CountSubdomains = %d", n)
	}

	id, err := s.CreateSiteUser(ctx, "client", "h", main, 3)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := s.UserByID(ctx, id)
	if u.Role != RoleSite || u.SiteID != main || u.MaxSubdomains != 3 || u.TOTPSecretEnc != nil {
		t.Errorf("site user = %+v", u)
	}
	if _, err := s.CreateSiteUser(ctx, "client2", "h", main, 3); err == nil {
		t.Error("second account for the same site was allowed")
	}
	if _, err := s.DB.Exec(`INSERT INTO users (username, password_hash, role, created_at) VALUES ('bad', 'h', 'site', 0)`); err == nil {
		t.Error("site role without a site was allowed")
	}
	// A main site with subdomains can't be deleted underneath them.
	if err := s.DeleteSite(ctx, main); err == nil {
		t.Error("deleted a main site that has subdomains")
	}
	s.DeleteSite(ctx, sub)
	if err := s.DeleteSite(ctx, main); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserByID(ctx, id); err != ErrNotFound {
		t.Errorf("site account outlived its site: %v", err)
	}
}
