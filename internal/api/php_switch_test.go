package api

import (
	"strconv"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/system"
)

// createSite makes a plain PHP site and waits for it.
func createSite(t *testing.T, c *client, domain, php string) int {
	t.Helper()
	st, body := c.do("POST", "/api/v1/sites", map[string]any{"domain": domain, "php_version": php})
	if st != 202 {
		t.Fatalf("create: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("create job: %v", job)
	}
	return int(body["site_id"].(float64))
}

func TestSwitchPHP(t *testing.T) {
	_, c, deps := siteEnv(t)
	id := createSite(t, c, "php.test", "8.3")
	path := "/api/v1/sites/" + strconv.Itoa(id) + "/php"

	if st, body := c.do("POST", path, map[string]string{"version": "8.3"}); st != 400 {
		t.Errorf("same version: %d %v", st, body)
	}
	if st, _ := c.do("POST", path, map[string]string{"version": "7.4"}); st != 400 {
		t.Errorf("not installed: %d", st)
	}

	st, body := c.do("POST", path, map[string]string{"version": "8.2"})
	if st != 202 {
		t.Fatalf("switch: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("switch job: %v", job)
	}
	_, site := c.do("GET", "/api/v1/sites/"+strconv.Itoa(id), nil)
	if site["php_version"] != "8.2" || site["fpm_pool"] != "/etc/php/8.2/fpm/pool.d/layte-php.test.conf" {
		t.Errorf("site: %v", site)
	}
	if !exists(t, deps.FS, "/etc/php/8.2/fpm/pool.d/layte-php.test.conf") || exists(t, deps.FS, "/etc/php/8.3/fpm/pool.d/layte-php.test.conf") {
		t.Error("pools not switched")
	}
	vhost, _ := deps.FS.ReadFile("/etc/nginx/sites-available/layte-php.test.conf")
	if !strings.Contains(string(vhost), "layte-php.test-php8.2.sock") {
		t.Errorf("vhost not switched:\n%s", vhost)
	}
}

func TestSwitchPHPRollsBack(t *testing.T) {
	_, c, deps := siteEnv(t)
	id := createSite(t, c, "php.test", "8.3")
	before, _ := deps.FS.ReadFile("/etc/nginx/sites-available/layte-php.test.conf")

	// nginx rejects the new vhost → pool for 8.2 removed, vhost restored.
	fr := deps.Runner.(*system.FakeRunner)
	orig := fr.Handle
	fr.Handle = func(cmd system.Cmd) (system.Result, error) {
		if cmd.String() == "/usr/sbin/nginx -t" {
			return system.Result{Stderr: []byte("nginx: [emerg] boom")}, &system.ExitError{Bin: cmd.Bin, ExitCode: 1}
		}
		return orig(cmd)
	}
	_, body := c.do("POST", "/api/v1/sites/"+strconv.Itoa(id)+"/php", map[string]string{"version": "8.2"})
	job := waitJob(t, c, body["job_id"].(string))
	if job["status"] != "failed" {
		t.Fatalf("job: %v", job)
	}
	_, site := c.do("GET", "/api/v1/sites/"+strconv.Itoa(id), nil)
	if site["php_version"] != "8.3" || site["status"] != "active" {
		t.Errorf("site changed: %v", site)
	}
	if exists(t, deps.FS, "/etc/php/8.2/fpm/pool.d/layte-php.test.conf") || !exists(t, deps.FS, "/etc/php/8.3/fpm/pool.d/layte-php.test.conf") {
		t.Error("pools not rolled back")
	}
	after, _ := deps.FS.ReadFile("/etc/nginx/sites-available/layte-php.test.conf")
	if string(after) != string(before) {
		t.Error("vhost not restored")
	}
}

func TestSwitchPHPImportedRefused(t *testing.T) {
	_, c, _ := siteEnv(t)
	_, scan := c.do("POST", "/api/v1/sites/scan", nil)
	id := int(scan["sites"].([]any)[0].(map[string]any)["id"].(float64))
	if st, body := c.do("POST", "/api/v1/sites/"+strconv.Itoa(id)+"/php", map[string]string{"version": "8.2"}); st != 409 || errCode(body) != "site_unmanaged" {
		t.Errorf("%d %v", st, body)
	}
}
