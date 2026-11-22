package api

import (
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/system"
)

func dbByName(t *testing.T, c *client) map[string]map[string]any {
	t.Helper()
	st, body := c.do("GET", "/api/v1/databases", nil)
	if st != 200 {
		t.Fatalf("list: %d %v", st, body)
	}
	out := map[string]map[string]any{}
	for _, x := range body["databases"].([]any) {
		d := x.(map[string]any)
		out[d["name"].(string)] = d
	}
	return out
}

func TestDatabases(t *testing.T) {
	_, c, deps := siteEnv(t)

	dbs := dbByName(t, c)
	if dbs["blog"]["managed"] != false || dbs["mysql"]["system"] != true {
		t.Errorf("initial list: %v", dbs)
	}
	// Foreign databases are read-only.
	if st, body := c.do("DELETE", "/api/v1/databases/blog", map[string]string{"confirm": "blog"}); st != 409 || errCode(body) != "db_unmanaged" {
		t.Errorf("drop foreign: %d %v", st, body)
	}
	if st, _ := c.do("POST", "/api/v1/databases/blog/password", nil); st != 409 {
		t.Errorf("reset foreign: %d", st)
	}

	// Create → password once, in stdin only.
	st, body := c.do("POST", "/api/v1/databases", map[string]any{"name": "analytics"})
	if st != 201 {
		t.Fatalf("create: %d %v", st, body)
	}
	pw := body["secrets"].(map[string]any)["db_password"].(string)
	for _, cmd := range deps.Runner.(*system.FakeRunner).Cmds {
		if strings.Contains(cmd.String(), pw) {
			t.Error("password in argv")
		}
	}
	if st, body := c.do("POST", "/api/v1/databases", map[string]any{"name": "analytics"}); st != 409 {
		t.Errorf("duplicate: %d %v", st, body)
	}
	for _, bad := range []string{"mysql", "Bad-Name", "x;drop"} {
		if st, _ := c.do("POST", "/api/v1/databases", map[string]any{"name": bad}); st != 400 {
			t.Errorf("create %q: %d", bad, st)
		}
	}
	if d := dbByName(t, c)["analytics"]; d["managed"] != true || d["user"] != "analytics" {
		t.Errorf("analytics: %v", d)
	}

	st, body = c.do("POST", "/api/v1/databases/analytics/password", nil)
	if st != 200 || body["secrets"].(map[string]any)["db_password"] == pw {
		t.Errorf("reset: %d %v", st, body)
	}

	if st, _ := c.do("DELETE", "/api/v1/databases/analytics", map[string]string{"confirm": "wrong"}); st != 400 {
		t.Errorf("bad confirm: %d", st)
	}
	if st, _ := c.do("DELETE", "/api/v1/databases/analytics", map[string]string{"confirm": "analytics"}); st != 204 {
		t.Errorf("drop: %d", st)
	}
	if _, ok := dbByName(t, c)["analytics"]; ok {
		t.Error("still listed after drop")
	}

	// A WordPress site's database: listed with its site, protected.
	_, b := c.do("POST", "/api/v1/sites", map[string]any{"domain": "wp.test", "php_version": "8.3",
		"wordpress": map[string]string{"title": "WP", "admin_user": "owner", "admin_email": "o@example.com"}})
	waitJob(t, c, b["job_id"].(string))
	if d := dbByName(t, c)["wptest"]; d["site_domain"] != "wp.test" || d["wordpress"] != true || d["managed"] != true {
		t.Errorf("site db: %v", d)
	}
	// The dev stub wp-config.php names no database: refused, nothing changed.
	if st, body := c.do("POST", "/api/v1/databases/wptest/password", nil); st != 409 || errCode(body) != "db_password_refused" {
		t.Errorf("reset WP db with stub wp-config: %d %v", st, body)
	}
	// A real wp-config.php: MySQL and wp-config.php change together.
	cfg := "/home/wptest/wp.test/public/wp-config.php"
	deps.FS.WriteFileAtomic(cfg, []byte("<?php\ndefine( 'DB_NAME', 'wptest' );\ndefine( 'DB_USER', 'wptest' );\ndefine( 'DB_PASSWORD', 'OLDpassword123456789012345' );\n"), 0o640)
	st, body = c.do("POST", "/api/v1/databases/wptest/password", nil)
	if st != 200 {
		t.Fatalf("reset WP db: %d %v", st, body)
	}
	newPW := body["secrets"].(map[string]any)["db_password"].(string)
	if b, _ := deps.FS.ReadFile(cfg); !strings.Contains(string(b), "define( 'DB_PASSWORD', '"+newPW+"' );") || strings.Contains(string(b), "OLDpassword") {
		t.Errorf("wp-config.php after reset:\n%s", b)
	}
	var altered bool
	for _, cmd := range deps.Runner.(*system.FakeRunner).Cmds {
		altered = altered || strings.Contains(string(cmd.Stdin), "ALTER USER 'wptest'@'localhost' IDENTIFIED BY '"+newPW+"'")
		if strings.Contains(cmd.String(), newPW) || strings.Contains(cmd.String(), "OLDpassword") {
			t.Error("password in argv")
		}
	}
	if !altered {
		t.Error("MySQL password not changed")
	}
	if st, body := c.do("DELETE", "/api/v1/databases/wptest", map[string]string{"confirm": "wptest"}); st != 409 || errCode(body) != "db_in_use" {
		t.Errorf("drop site db: %d %v", st, body)
	}
}
