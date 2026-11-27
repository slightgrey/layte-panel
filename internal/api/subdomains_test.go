package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestSiteAccountSubdomains: a site account adds subdomain sites on its
// main site's Linux user, within its limit, with one SFTP login for all;
// deleting a subdomain site removes only its folder.
func TestSiteAccountSubdomains(t *testing.T) {
	env, admin, deps := siteEnv(t)
	mainID := int64(createSite(t, admin, "acme.test", "8.3"))
	user := "acmetest"
	ctx := context.Background()

	// Admin: SFTP on for the main site, then a login with a limit of 1.
	if code, body := admin.do("PUT", fmt.Sprintf("/api/v1/sites/%d/sftp", mainID), map[string]any{"enabled": true, "keys": []string{sftpTestKey}}); code != 200 {
		t.Fatalf("sftp: %d %v", code, body)
	}
	c := env.siteUser(t, "acme", mainID)
	env.st.SetMaxSubdomains(ctx, mustUser(t, env, "acme"), 1) // read on every request

	for _, bad := range []map[string]any{
		{"domain": "other.test", "php_version": "8.3"},          // not a subdomain
		{"domain": "a.b.acme.test", "php_version": "8.3"},       // two levels
		{"domain": "acme.test.evil.test", "php_version": "8.3"}, // suffix trick
	} {
		if code, body := c.do("POST", "/api/v1/sites", bad); code != 400 {
			t.Errorf("%v: %d %v", bad["domain"], code, body)
		}
	}
	code, body := c.do("POST", "/api/v1/sites", map[string]any{
		"domain": "blog.acme.test", "php_version": "8.3", "create_db": true,
		"aliases": []string{"www.other.test"}, "username": "root", // ignored for site accounts
	})
	if code != 202 {
		t.Fatalf("create subdomain: %d %v", code, body)
	}
	subID := int64(body["site_id"].(float64))
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("job: %v", job)
	}
	sub, _ := env.st.SiteByID(ctx, subID)
	if sub.SystemUser != user || sub.ParentSiteID != mainID || len(sub.Aliases) != 0 || sub.DBName != "acmetest_blog" {
		t.Errorf("subdomain site: %+v", sub)
	}
	if !exists(t, deps.FS, "/home/acmetest/blog.acme.test/public") {
		t.Error("subdomain folder missing")
	}
	// One SFTP login, both folders.
	if !exists(t, deps.FS, "/etc/systemd/system/layte-sftp-acmetest.blog.acme.test.service") {
		t.Error("no SFTP mount for the subdomain")
	}
	_, st := c.do("GET", fmt.Sprintf("/api/v1/sites/%d/sftp", subID), nil)
	if st["site_id"] != float64(mainID) || len(st["folders"].([]any)) != 2 {
		t.Errorf("subdomain sftp state: %v", st)
	}

	// The limit.
	if code, body := c.do("POST", "/api/v1/sites", map[string]any{"domain": "shop.acme.test", "php_version": "8.3"}); code != 409 || errCode(body) != "limit_reached" {
		t.Errorf("over the limit: %d %v", code, body)
	}
	// The admin can't delete the main site while it has a subdomain.
	if code, body := admin.do("DELETE", fmt.Sprintf("/api/v1/sites/%d", mainID), map[string]any{"confirm_domain": "acme.test", "remove_files": true}); code != 409 || errCode(body) != "has_subdomains" {
		t.Errorf("delete main with subdomains: %d %v", code, body)
	}

	// The account deletes its subdomain: the folder goes, the user stays.
	code, body = c.do("DELETE", "/api/v1/sites/"+strconv.FormatInt(subID, 10), map[string]any{"confirm_domain": "blog.acme.test", "remove_files": true, "drop_db": true})
	if code != 202 {
		t.Fatalf("delete subdomain: %d %v", code, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("delete job: %v", job)
	}
	if exists(t, deps.FS, "/home/acmetest/blog.acme.test") {
		t.Error("subdomain folder left")
	}
	if !exists(t, deps.FS, "/home/acmetest/acme.test/public") || !exists(t, deps.FS, "/home/acmetest/.layte-site-user") {
		t.Error("main site's files or user removed")
	}
	if exists(t, deps.FS, "/etc/systemd/system/layte-sftp-acmetest.blog.acme.test.service") {
		t.Error("SFTP mount for the deleted subdomain left")
	}
	if !exists(t, deps.FS, "/etc/systemd/system/layte-sftp-acmetest.service") {
		t.Error("main site's SFTP mount removed")
	}
}

func mustUser(t *testing.T, env *testEnv, name string) int64 {
	t.Helper()
	u, err := env.st.UserByUsername(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// A name that a hand-written nginx config serves (never scanned into Layte),
// or that a wildcard covers, or the panel's own address can't become a site.
func TestNewSiteCantTakeNginxNames(t *testing.T) {
	env, admin, deps := siteEnv(t)
	mainID := int64(createSite(t, admin, "acme.test", "8.3"))
	conf := "server {\n  listen 80;\n  server_name panel.acme.test;\n  location / { proxy_pass http://127.0.0.1:8080; }\n}\n" +
		"server {\n  listen 80;\n  server_name *.wild.test;\n  root /var/www/wild;\n}\n"
	if err := deps.FS.WriteFileAtomic("/etc/nginx/conf.d/hand-written.conf", []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	c := env.siteUser(t, "acme", mainID)
	if code, body := c.do("POST", "/api/v1/sites", map[string]any{"domain": "panel.acme.test", "php_version": "8.3"}); code != 409 || errCode(body) != "domain_taken" {
		t.Errorf("site login takes the panel's name: %d %v", code, body)
	}
	if code, body := admin.do("POST", "/api/v1/sites", map[string]any{"domain": "x.wild.test", "php_version": "8.3"}); code != 409 || errCode(body) != "domain_taken" {
		t.Errorf("admin takes a wildcard's name: %d %v", code, body)
	}
	if code, body := c.do("POST", "/api/v1/sites", map[string]any{"domain": "shop.acme.test", "php_version": "8.3"}); code != 202 {
		t.Errorf("a free subdomain was refused: %d %v", code, body)
	}
}

func TestPanelHostsReserved(t *testing.T) {
	r := httptest.NewRequest("POST", "https://Panel.Example.com:8443/api/v1/sites", nil)
	if got := panelHosts(r); len(got) != 1 || got[0] != "panel.example.com" {
		t.Errorf("panelHosts = %v", got)
	}
	r = httptest.NewRequest("POST", "http://127.0.0.1:8080/api/v1/sites", nil)
	if got := panelHosts(r); len(got) != 0 {
		t.Errorf("an IP address was reserved: %v", got)
	}
}
