package api

import (
	"strconv"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/system"
)

func TestObjectCache(t *testing.T) {
	_, c, deps := siteEnv(t)
	_, b := c.do("POST", "/api/v1/sites", map[string]any{"domain": "oc.test", "php_version": "8.3",
		"wordpress": map[string]string{"title": "O", "admin_user": "owner", "admin_email": "o@example.com"}})
	if job := waitJob(t, c, b["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("create: %v", job)
	}
	id := strconv.Itoa(int(b["site_id"].(float64)))
	path := "/api/v1/sites/" + id + "/object-cache"
	_, site := c.do("GET", "/api/v1/sites/"+id, nil)
	user := site["system_user"].(string)
	dropIn := "/home/" + user + "/oc.test/public/wp-content/object-cache.php"

	if st, _ := c.do("POST", path+"/flush", nil); st != 400 {
		t.Errorf("flush while off: %d", st)
	}
	if _, st := c.do("GET", path, nil); st["enabled"] != false {
		t.Errorf("initial: %v", st)
	}

	// On: Redis installed, the site's own instance, the drop-in.
	code, body := c.do("PUT", path, map[string]any{"enabled": true})
	if code != 202 {
		t.Fatalf("enable: %d %v", code, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("enable job: %v", job)
	}
	if b, _ := deps.FS.ReadFile(dropIn); !strings.Contains(string(b), "Redis Object Cache Drop-In") {
		t.Errorf("drop-in: %q", b)
	}
	_, st := c.do("GET", path, nil)
	if st["enabled"] != true || st["installed_plugin"] != true || st["running"] != true || st["keys"] != float64(42) || st["limit_bytes"] != float64(32<<20) {
		t.Errorf("state: %v", st)
	}
	if st, _ := c.do("POST", path+"/flush", nil); st != 204 {
		t.Errorf("flush: %d", st)
	}

	// A PHP switch installs Redis's extension for the new version first.
	_, body = c.do("POST", "/api/v1/sites/"+id+"/php", map[string]string{"version": "8.2"})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("php switch: %v", job)
	}
	if calls := strings.Join(deps.Runner.(*system.FakeRunner).CallList(), "\n"); !strings.Contains(calls, "install php8.2-redis") {
		t.Errorf("no php8.2-redis install in:\n%s", calls)
	}

	// Off: drop-in gone, plugin deleted (Layte installed it).
	_, body = c.do("PUT", path, map[string]any{"enabled": false})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("disable job: %v", job)
	}
	if ok, _ := deps.FS.Exists(dropIn); ok {
		t.Error("drop-in left")
	}
	if _, st := c.do("GET", path, nil); st["enabled"] != false {
		t.Errorf("after off: %v", st)
	}
	if code, _ := c.do("PUT", path, map[string]any{"enabled": false}); code != 400 {
		t.Errorf("off twice: %d", code)
	}

	// Not WordPress: refused.
	plain := strconv.Itoa(createSite(t, c, "plain-oc.test", "8.3"))
	if code, _ := c.do("PUT", "/api/v1/sites/"+plain+"/object-cache", map[string]any{"enabled": true}); code != 400 {
		t.Errorf("plain site: %d", code)
	}
}
