package api

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/system"
)

type storeAudit struct{ st *store.Store }

func (a storeAudit) Audit(ctx context.Context, e agent.AuditEntry) error {
	return a.st.InsertAudit(ctx, store.AuditRow{Time: e.Time, UserID: e.Actor.UserID, Username: e.Actor.Username,
		Action: e.Action, ArgsJSON: e.ArgsJSON, OK: e.OK, Error: e.Error})
}

// siteEnv is a logged-in client plus an in-process agent on a fake root.
func siteEnv(t *testing.T) (*testEnv, *client, actions.Deps) {
	t.Helper()
	ag, deps := devAgent(t)
	env := newTestEnvWithAgent(t, ag)
	ag.Auditor = storeAudit{env.st}
	return env, env.loggedIn(t), deps
}

// waitJob polls until the job leaves "running".
func waitJob(t *testing.T, c *client, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, body := c.do("GET", "/api/v1/jobs/"+id, nil)
		if body["status"] != "running" {
			return body
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return nil
}

func exists(t *testing.T, fs system.FS, p string) bool {
	t.Helper()
	ok, err := fs.Exists(p)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

var wpSite = map[string]any{
	"domain":      "Shop.Test",
	"aliases":     []string{"www.shop.test"},
	"php_version": "8.3",
	"wordpress":   map[string]string{"title": "My Shop", "admin_user": "owner", "admin_email": "owner@example.com"},
}

func TestCreateAndDeleteWordPressSite(t *testing.T) {
	env, c, deps := siteEnv(t)

	st, body := c.do("POST", "/api/v1/sites", wpSite)
	if st != 202 {
		t.Fatalf("create: %d %v", st, body)
	}
	jobID := body["job_id"].(string)
	siteID := int(body["site_id"].(float64))
	job := waitJob(t, c, jobID)
	if job["status"] != "succeeded" || job["has_secret"] != true {
		t.Fatalf("job: %v", job)
	}

	// The site row is active and managed, with the normalised domain.
	_, site := c.do("GET", "/api/v1/sites/"+strconv.Itoa(siteID), nil)
	if site["domain"] != "shop.test" || site["status"] != "active" || site["managed"] != true ||
		site["system_user"] != "shoptest" || site["db_name"] != "shoptest" || site["wordpress"] != true {
		t.Fatalf("site: %v", site)
	}

	// Files on the fake root.
	for _, p := range []string{
		"/etc/nginx/sites-available/layte-shop.test.conf",
		"/etc/nginx/sites-enabled/layte-shop.test.conf",
		"/etc/php/8.3/fpm/pool.d/layte-shop.test.conf",
		"/home/shoptest/shop.test/public/wp-config.php",
		"/home/shoptest/shop.test/logs",
	} {
		if !exists(t, deps.FS, p) {
			t.Errorf("missing %s", p)
		}
	}
	vhost, _ := deps.FS.ReadFile("/etc/nginx/sites-enabled/layte-shop.test.conf")
	if !strings.Contains(string(vhost), "server_name shop.test www.shop.test;") ||
		!strings.Contains(string(vhost), "fastcgi_pass unix:/run/php/layte-shop.test-php8.3.sock;") {
		t.Errorf("vhost:\n%s", vhost)
	}

	// Credentials: once, to the creator.
	_, sec := c.do("GET", "/api/v1/jobs/"+jobID+"/secret", nil)
	secrets := sec["secrets"].(map[string]any)
	dbPW, wpPW := secrets["db_password"].(string), secrets["wp_admin_password"].(string)
	if len(dbPW) != 32 || len(wpPW) != 32 || secrets["wp_admin_user"] != "owner" {
		t.Fatalf("secrets: %v", secrets)
	}
	if st, _ := c.do("GET", "/api/v1/jobs/"+jobID+"/secret", nil); st != 404 {
		t.Error("secret readable twice")
	}

	// Secrets went over stdin only: never in argv, never in the audit log or job record.
	fr := deps.Runner.(*system.FakeRunner)
	sawStdin := false
	for _, cmd := range fr.Cmds {
		if strings.Contains(cmd.String(), dbPW) || strings.Contains(cmd.String(), wpPW) {
			t.Errorf("password in argv: %s", cmd.String())
		}
		if strings.Contains(string(cmd.Stdin), dbPW) {
			sawStdin = true
		}
	}
	if !sawStdin {
		t.Error("db password was never sent on stdin")
	}
	var audit strings.Builder
	rows, _ := env.st.DB.Query(`SELECT action || ' ' || args_json FROM audit_log`)
	n := 0
	for rows.Next() {
		var s string
		rows.Scan(&s)
		audit.WriteString(s + "\n")
		n++
	}
	rows.Close()
	if n < 6 || strings.Contains(audit.String(), dbPW) || strings.Contains(audit.String(), wpPW) || !strings.Contains(audit.String(), "[redacted]") {
		t.Errorf("audit log (%d rows):\n%s", n, audit.String())
	}
	jobRow, _ := env.st.JobByID(context.Background(), jobID)
	if strings.Contains(jobRow.EventsJSON+jobRow.ResultJSON, dbPW) {
		t.Error("secret stored in job record")
	}

	// Delete: wrong confirmation is refused, then everything is removed.
	if st, _ := c.do("DELETE", "/api/v1/sites/"+strconv.Itoa(siteID), map[string]any{"confirm_domain": "nope"}); st != 400 {
		t.Errorf("bad confirm: %d", st)
	}
	st, body = c.do("DELETE", "/api/v1/sites/"+strconv.Itoa(siteID),
		map[string]any{"confirm_domain": "shop.test", "remove_files": true, "drop_db": true})
	if st != 202 {
		t.Fatalf("delete: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("delete job: %v", job)
	}
	for _, p := range []string{
		"/etc/nginx/sites-available/layte-shop.test.conf",
		"/etc/nginx/sites-enabled/layte-shop.test.conf",
		"/etc/php/8.3/fpm/pool.d/layte-shop.test.conf",
		"/home/shoptest",
	} {
		if exists(t, deps.FS, p) {
			t.Errorf("%s still exists", p)
		}
	}
	if st, _ := c.do("GET", "/api/v1/sites/"+strconv.Itoa(siteID), nil); st != 404 {
		t.Errorf("site row still there: %d", st)
	}
}

func TestCreateRollsBackOnConfigTestFailure(t *testing.T) {
	_, c, deps := siteEnv(t)
	fr := deps.Runner.(*system.FakeRunner)
	orig := fr.Handle
	fr.Handle = func(cmd system.Cmd) (system.Result, error) {
		if cmd.String() == "/usr/sbin/nginx -t" {
			return system.Result{Stderr: []byte(`nginx: [emerg] unknown directive "oops"`)}, &system.ExitError{Bin: cmd.Bin, ExitCode: 1}
		}
		return orig(cmd)
	}

	_, body := c.do("POST", "/api/v1/sites", map[string]any{"domain": "broken.test", "php_version": "8.2", "create_db": true})
	job := waitJob(t, c, body["job_id"].(string))
	errMsg, _ := job["error"].(string)
	if job["status"] != "failed" || !strings.Contains(errMsg, "unknown directive") || !strings.Contains(errMsg, "rolled back") {
		t.Fatalf("job: %v", job)
	}
	for _, p := range []string{"/etc/nginx/sites-available/layte-broken.test.conf", "/etc/php/8.2/fpm/pool.d/layte-broken.test.conf", "/home/brokentest"} {
		if exists(t, deps.FS, p) {
			t.Errorf("%s not rolled back", p)
		}
	}
	// Fully rolled back → no site row left; the DB step never ran.
	_, list := c.do("GET", "/api/v1/sites", nil)
	if n := len(list["sites"].([]any)); n != 0 {
		t.Errorf("sites = %d", n)
	}
	for _, call := range fr.CallList() {
		if strings.HasPrefix(call, "/usr/bin/mysql --batch") {
			t.Error("database created despite earlier failure")
		}
	}
}

func TestCreateSiteValidationAndConflicts(t *testing.T) {
	_, c, _ := siteEnv(t)
	c.do("POST", "/api/v1/sites/scan", nil) // imports blog.example.com etc.

	tests := []struct {
		name string
		body map[string]any
		code string
	}{
		{"bad domain", map[string]any{"domain": "../etc", "php_version": "8.3"}, "invalid"},
		{"php not installed", map[string]any{"domain": "a.test", "php_version": "7.4"}, "invalid"},
		{"taken by imported site", map[string]any{"domain": "blog.example.com", "php_version": "8.3"}, "domain_taken"},
		{"alias taken", map[string]any{"domain": "new.test", "aliases": []string{"www.blog.example.com"}, "php_version": "8.3"}, "domain_taken"},
		{"reserved username", map[string]any{"domain": "b.test", "username": "root", "php_version": "8.3"}, "invalid"},
		{"bad wp email", map[string]any{"domain": "c.test", "php_version": "8.3", "wordpress": map[string]string{"title": "x", "admin_user": "owner", "admin_email": "nope"}}, "invalid"},
		{"unknown field", map[string]any{"domain": "d.test", "php_version": "8.3", "shell": "rm -rf /"}, "bad_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, body := c.do("POST", "/api/v1/sites", tt.body)
			if st < 400 || errCode(body) != tt.code {
				t.Errorf("%d %v, want code %s", st, body, tt.code)
			}
		})
	}

	// Imported sites can't be deleted.
	_, list := c.do("GET", "/api/v1/sites", nil)
	id := int(list["sites"].([]any)[0].(map[string]any)["id"].(float64))
	st, body := c.do("DELETE", "/api/v1/sites/"+strconv.Itoa(id), map[string]any{"confirm_domain": "blog.example.com"})
	if st != 409 || errCode(body) != "site_unmanaged" {
		t.Errorf("delete imported: %d %v", st, body)
	}
}

func TestSecondSiteGetsNumberedUsername(t *testing.T) {
	_, c, _ := siteEnv(t)
	_, b1 := c.do("POST", "/api/v1/sites", map[string]any{"domain": "shop.test", "php_version": "8.3"})
	waitJob(t, c, b1["job_id"].(string))
	// "s-hop.test" also derives "shoptest", so it gets the next free name.
	_, b2 := c.do("POST", "/api/v1/sites", map[string]any{"domain": "s-hop.test", "php_version": "8.3"})
	waitJob(t, c, b2["job_id"].(string))
	_, s2 := c.do("GET", "/api/v1/sites/"+strconv.Itoa(int(b2["site_id"].(float64))), nil)
	if s2["system_user"] != "shoptest2" {
		t.Errorf("user = %v", s2["system_user"])
	}
}
