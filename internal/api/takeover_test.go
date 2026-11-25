package api

import (
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/system"
)

func TestTakeoverAdoptedSite(t *testing.T) {
	_, c, deps := siteEnv(t)
	c.do("POST", "/api/v1/sites/scan", nil)
	shop := siteIDByDomain(t, c, "shop.example.org")

	// Not adopted yet: no takeover, and no PHP switch.
	if st, body := c.do("GET", "/api/v1/sites/"+shop+"/takeover", nil); st != 409 || errCode(body) != "site_not_adopted" {
		t.Errorf("preview before adopting: %d %v", st, body)
	}
	_, prev := c.do("GET", "/api/v1/sites/"+shop+"/adopt", nil)
	_, body := c.do("POST", "/api/v1/sites/"+shop+"/adopt", map[string]string{"username": "shopexampleorg", "pool_hash": prev["pool_hash"].(string)})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("adopt job: %v", job)
	}
	if st, body := c.do("POST", "/api/v1/sites/"+shop+"/php", map[string]string{"version": "8.3"}); st != 409 {
		t.Errorf("PHP switch before takeover: %d %v", st, body)
	}

	st, tp := c.do("GET", "/api/v1/sites/"+shop+"/takeover", nil)
	if st != 200 || tp["vhost_hash"] == "" || tp["new_vhost_file"] != "/etc/nginx/sites-available/layte-shop.example.org.conf" {
		t.Fatalf("takeover preview: %d %v", st, tp)
	}
	if st, body := c.do("POST", "/api/v1/sites/"+shop+"/takeover", map[string]string{}); st != 400 {
		t.Errorf("no hash: %d %v", st, body)
	}
	st, body = c.do("POST", "/api/v1/sites/"+shop+"/takeover", map[string]string{"vhost_hash": tp["vhost_hash"].(string)})
	if st != 202 {
		t.Fatalf("takeover: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("takeover job: %v", job)
	}
	_, site := c.do("GET", "/api/v1/sites/"+shop, nil)
	if site["nginx_managed"] != true || site["adopted"] != true || site["managed"] != false ||
		site["fpm_pool"] != "/etc/php/8.2/fpm/pool.d/layte-shop.example.org.conf" {
		t.Errorf("site after takeover: %v", site)
	}
	// A rescan keeps it as it is.
	c.do("POST", "/api/v1/sites/scan", nil)
	if _, site := c.do("GET", "/api/v1/sites/"+shop, nil); site["nginx_managed"] != true || site["root_path"] != "/var/www/shop/public" {
		t.Errorf("after rescan: %v", site)
	}
	if st, _ := c.do("GET", "/api/v1/sites/"+shop+"/takeover", nil); st != 409 {
		t.Errorf("preview again: %d", st)
	}

	// Now PHP can be switched; files stay in the web root.
	st, body = c.do("POST", "/api/v1/sites/"+shop+"/php", map[string]string{"version": "8.3"})
	if st != 202 {
		t.Fatalf("PHP switch: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("PHP switch job: %v", job)
	}
	vhost, _ := deps.FS.ReadFile("/etc/nginx/sites-available/layte-shop.example.org.conf")
	if !system.IsManaged(vhost) || !strings.Contains(string(vhost), "root /var/www/shop/public;") || !strings.Contains(string(vhost), "php8.3.sock") {
		t.Errorf("vhost after switch:\n%s", vhost)
	}
	if ok, _ := deps.FS.Exists("/etc/php/8.2/fpm/pool.d/layte-shop.example.org.conf"); ok {
		t.Error("old pool left behind")
	}
	// The shop isn't WordPress: the page cache says so (not "unmanaged").
	if st, body := c.do("PUT", "/api/v1/sites/"+shop+"/page-cache", map[string]any{"enabled": true}); st != 400 {
		t.Errorf("page cache on non-WordPress: %d %v", st, body)
	}
	// Files and cron still work as for an adopted site.
	if st, body := c.do("POST", "/api/v1/sites/"+shop+"/files/mkdir", map[string]string{"path": "uploads"}); st != 204 {
		t.Errorf("mkdir after takeover: %d %v", st, body)
	}
}
