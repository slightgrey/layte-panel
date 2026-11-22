package api

import (
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/system"
)

func TestDatabaseExportImport(t *testing.T) {
	_, c, deps := siteEnv(t)
	c.do("POST", "/api/v1/sites/scan", nil)
	_, b := c.do("POST", "/api/v1/sites", map[string]any{"domain": "dump.test", "php_version": "8.3", "create_db": true})
	if job := waitJob(t, c, b["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("create site: %v", job)
	}
	runner := deps.Runner.(*system.FakeRunner)

	// Export → a file in the site's tmp/, written by mysqldump as the site user.
	st, body := c.do("POST", "/api/v1/databases/dumptest/export", nil)
	if st != 202 {
		t.Fatalf("export: %d %v", st, body)
	}
	job := waitJob(t, c, body["job_id"].(string))
	if job["status"] != "succeeded" {
		t.Fatalf("export job: %v", job)
	}
	rel := job["result"].(map[string]any)["path"].(string)
	if !strings.HasPrefix(rel, "tmp/dumptest-") || !strings.HasSuffix(rel, ".sql") {
		t.Fatalf("dump path %q", rel)
	}
	if !exists(t, deps.FS, "/home/dumptest/dump.test/"+rel) {
		t.Fatal("dump file not written")
	}
	var dump system.Cmd
	var created, dropped bool
	for _, cmd := range runner.Cmds {
		in := string(cmd.Stdin)
		if cmd.Bin == "/usr/bin/mysqldump" {
			dump = cmd
		}
		created = created || strings.Contains(in, "CREATE USER 'layte_tmp_") && strings.Contains(in, "ON `dumptest`.*")
		dropped = dropped || strings.Contains(in, "DROP USER IF EXISTS 'layte_tmp_")
	}
	if dump.As == nil || !strings.Contains(dump.String(), "--result-file=/home/dumptest/dump.test/tmp/") {
		t.Errorf("mysqldump must run as the site user into tmp/: %s (as %v)", dump, dump.As)
	}
	if strings.Contains(dump.String(), "MYSQL_PWD") || strings.Contains(dump.String(), "password") {
		t.Errorf("password in argv: %s", dump)
	}
	if !created || !dropped {
		t.Errorf("temporary user created=%v dropped=%v", created, dropped)
	}

	// Import needs the typed name and a tmp/*.sql path.
	for _, tc := range []struct {
		req  map[string]string
		code int
	}{
		{map[string]string{"path": rel, "confirm": "nope"}, 400},
		{map[string]string{"path": "../../etc/shadow.sql", "confirm": "dumptest"}, 400},
		{map[string]string{"path": "public/wp-config.php", "confirm": "dumptest"}, 400},
		{map[string]string{"path": "tmp/../x.sql", "confirm": "dumptest"}, 400},
	} {
		if st, body := c.do("POST", "/api/v1/databases/dumptest/import", tc.req); st != tc.code {
			t.Errorf("import %v: %d %v", tc.req, st, body)
		}
	}
	st, body = c.do("POST", "/api/v1/databases/dumptest/import", map[string]string{"path": rel, "confirm": "dumptest"})
	if st != 202 {
		t.Fatalf("import: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("import job: %v", job)
	}
	var imp system.Cmd
	for _, cmd := range runner.Cmds {
		if cmd.Bin == "/usr/bin/mysql" && strings.Contains(cmd.String(), "--execute=source") {
			imp = cmd
		}
	}
	if imp.As == nil || !strings.Contains(imp.String(), "--local-infile=0") || !strings.Contains(imp.String(), "--database=dumptest") {
		t.Errorf("import command: %s (as %v)", imp, imp.As)
	}
	// A missing file fails the job, not the request.
	_, body = c.do("POST", "/api/v1/databases/dumptest/import", map[string]string{"path": "tmp/missing.sql", "confirm": "dumptest"})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "failed" {
		t.Errorf("missing file: %v", job)
	}

	// Only Layte databases linked to a site.
	c.do("POST", "/api/v1/databases", map[string]any{"name": "loose"})
	if st, body := c.do("POST", "/api/v1/databases/loose/export", nil); st != 409 || errCode(body) != "db_no_site" {
		t.Errorf("unlinked: %d %v", st, body)
	}
	if st, body := c.do("POST", "/api/v1/databases/blog/export", nil); st != 409 || errCode(body) != "db_unmanaged" {
		t.Errorf("foreign: %d %v", st, body)
	}
}
