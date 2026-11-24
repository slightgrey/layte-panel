package api

import (
	"strconv"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/pagecache"
)

func TestPageCache(t *testing.T) {
	_, c, deps := siteEnv(t)
	_, b := c.do("POST", "/api/v1/sites", map[string]any{"domain": "cache.test", "php_version": "8.3",
		"wordpress": map[string]string{"title": "C", "admin_user": "owner", "admin_email": "o@example.com"}})
	if job := waitJob(t, c, b["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("create: %v", job)
	}
	id := strconv.Itoa(int(b["site_id"].(float64)))
	path := "/api/v1/sites/" + id + "/page-cache"
	vhostPath := "/etc/nginx/sites-available/layte-cache.test.conf"
	var user string
	_, site := c.do("GET", "/api/v1/sites/"+id, nil)
	user = site["system_user"].(string)
	plugin := "/home/" + user + "/cache.test/public/" + pagecache.PluginRel

	if st, _ := c.do("POST", path+"/purge", nil); st != 400 {
		t.Errorf("purge while off: %d", st)
	}
	if st, _ := c.do("PUT", path, map[string]any{"enabled": true, "ttl_minutes": 5000}); st != 400 {
		t.Errorf("bad ttl: %d", st)
	}

	// On.
	st, body := c.do("PUT", path, map[string]any{"enabled": true, "ttl_minutes": 30})
	if st != 202 {
		t.Fatalf("enable: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("enable job: %v", job)
	}
	v, _ := deps.FS.ReadFile(vhostPath)
	if !strings.Contains(string(v), "fastcgi_cache_path /var/cache/nginx/layte/cache.test") || !strings.Contains(string(v), "fastcgi_cache_valid 200 301 302 30m;") {
		t.Errorf("vhost:\n%s", v)
	}
	if p, _ := deps.FS.ReadFile(plugin); string(p) != pagecache.Plugin {
		t.Error("purge plugin not installed")
	}
	if ok, _ := deps.FS.Exists(pagecache.Root); !ok {
		t.Error("cache root folder not created (nginx doesn't create parents)")
	}

	// Other changes keep the cache in the vhost.
	st, body = c.do("PUT", "/api/v1/sites/"+id+"/php-settings", map[string]any{"upload_max": "128M"})
	if st != 202 {
		t.Fatalf("php settings: %d %v", st, body)
	}
	waitJob(t, c, body["job_id"].(string))
	if v, _ := deps.FS.ReadFile(vhostPath); !strings.Contains(string(v), "fastcgi_cache ") || !strings.Contains(string(v), "client_max_body_size 128m;") {
		t.Errorf("cache lost after PHP settings:\n%s", v)
	}

	deps.FS.MkdirAll(pagecache.Dir("cache.test")+"/x", 0o700)
	if st, _ := c.do("POST", path+"/purge", nil); st != 204 {
		t.Errorf("purge: %d", st)
	}
	if ok, _ := deps.FS.Exists(pagecache.Dir("cache.test") + "/x"); ok {
		t.Error("not purged")
	}

	// Off.
	st, body = c.do("PUT", path, map[string]any{"enabled": false})
	if st != 202 {
		t.Fatalf("disable: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("disable job: %v", job)
	}
	if v, _ := deps.FS.ReadFile(vhostPath); strings.Contains(string(v), "fastcgi_cache") {
		t.Errorf("cache still in vhost:\n%s", v)
	}
	if ok, _ := deps.FS.Exists(plugin); ok {
		t.Error("purge plugin not removed")
	}

	// Plain PHP sites can't use it.
	plain := createSite(t, c, "plain.test", "8.3")
	if st, _ := c.do("PUT", "/api/v1/sites/"+strconv.Itoa(plain)+"/page-cache", map[string]any{"enabled": true}); st != 400 {
		t.Errorf("non-WordPress site: %d", st)
	}
}
