package api

import (
	"context"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/actions"
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

// httpsAdoptedShop gives the shop an HTTPS vhost with its own certbot
// certificate, whose dotfile rule also blocks Let's Encrypt's challenge path
// (like many hand-written configs), adopts it and returns its takeover preview.
func httpsAdoptedShop(t *testing.T, c *client, deps actions.Deps) (string, map[string]any) {
	t.Helper()
	const vhost = "/etc/nginx/sites-enabled/shop.example.org.conf"
	b, _ := deps.FS.ReadFile(vhost)
	https := strings.Replace(string(b), "    listen 80;", `    listen 443 ssl;
    ssl_certificate /etc/letsencrypt/live/shop.example.org/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/shop.example.org/privkey.pem;
    location ~ /\. { deny all; }`, 1)
	deps.FS.WriteFileAtomic(vhost, []byte(https), 0o644)
	// The site's own certificate (the fake certbot writes a real one).
	if _, err := deps.Runner.Run(context.Background(), "/usr/bin/certbot", "certonly", "--cert-name", "shop.example.org",
		"--domains", "shop.example.org,www.shop.example.org"); err != nil {
		t.Fatal(err)
	}
	c.do("POST", "/api/v1/sites/scan", nil)
	shop := siteIDByDomain(t, c, "shop.example.org")
	_, prev := c.do("GET", "/api/v1/sites/"+shop+"/adopt", nil)
	_, body := c.do("POST", "/api/v1/sites/"+shop+"/adopt", map[string]string{"username": "shopexampleorg", "pool_hash": prev["pool_hash"].(string)})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("adopt job: %v", job)
	}
	_, tp := c.do("GET", "/api/v1/sites/"+shop+"/takeover", nil)
	if tp["ssl"] != true || tp["own_cert"] != "/etc/letsencrypt/live/shop.example.org" {
		t.Fatalf("preview: %v", tp)
	}
	return shop, tp
}

const (
	shopOwnCert   = "/etc/letsencrypt/live/shop.example.org/fullchain.pem"
	shopLayteCert = "/etc/letsencrypt/live/layte-shop.example.org/fullchain.pem"
	shopVhostPath = "/etc/nginx/sites-available/layte-shop.example.org.conf"
)

// The takeover keeps the site's certificate first, then switches to a Layte one.
func TestTakeoverSwitchesToLayteCertificate(t *testing.T) {
	_, c, deps := siteEnv(t)
	shop, tp := httpsAdoptedShop(t, c, deps)
	if st, _ := c.do("POST", "/api/v1/sites/"+shop+"/takeover", map[string]string{"vhost_hash": tp["vhost_hash"].(string)}); st != 400 {
		t.Errorf("HTTPS takeover without an email: %d", st)
	}
	_, body := c.do("POST", "/api/v1/sites/"+shop+"/takeover", map[string]any{"vhost_hash": tp["vhost_hash"].(string), "email": "admin@example.org", "delete_old_cert": true})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("takeover job: %v", job)
	}
	_, site := c.do("GET", "/api/v1/sites/"+shop, nil)
	if site["nginx_managed"] != true || site["cert_path"] != shopLayteCert {
		t.Errorf("site after takeover: %v", site)
	}
	// The old certificate was deleted (nothing uses it any more).
	if ok, _ := deps.FS.Exists("/etc/letsencrypt/live/shop.example.org"); ok {
		t.Error("old certificate not deleted")
	}
	if v, _ := deps.FS.ReadFile(shopVhostPath); !strings.Contains(string(v), "ssl_certificate     "+shopLayteCert+";") {
		t.Errorf("vhost doesn't use the Layte certificate:\n%s", v)
	}
	c.do("POST", "/api/v1/sites/scan", nil)
	if _, site := c.do("GET", "/api/v1/sites/"+shop, nil); site["cert_path"] != shopLayteCert {
		t.Errorf("after rescan: %v", site["cert_path"])
	}
}

// If the Layte certificate can't be issued, the site is still taken over and
// keeps serving HTTPS with its own certificate; later config writes keep it.
func TestTakeoverKeepsOwnCertificate(t *testing.T) {
	_, c, deps := siteEnv(t)
	shop, tp := httpsAdoptedShop(t, c, deps)
	certbot, _ := deps.FS.ReadFile("/usr/bin/certbot")
	deps.FS.Remove("/usr/bin/certbot") // issuing fails
	_, body := c.do("POST", "/api/v1/sites/"+shop+"/takeover", map[string]any{"vhost_hash": tp["vhost_hash"].(string), "email": "admin@example.org", "delete_old_cert": true})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("takeover job: %v", job)
	}
	_, site := c.do("GET", "/api/v1/sites/"+shop, nil)
	if site["nginx_managed"] != true || site["cert_path"] != shopOwnCert {
		t.Errorf("site after takeover: %v", site)
	}
	_, body = c.do("PUT", "/api/v1/sites/"+shop+"/php-settings", map[string]any{"upload_max": "128M"})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("php settings job: %v", job)
	}
	if v, _ := deps.FS.ReadFile(shopVhostPath); !strings.Contains(string(v), "ssl_certificate     "+shopOwnCert+";") ||
		!strings.Contains(string(v), "client_max_body_size 128m;") {
		t.Errorf("vhost lost the site's own certificate:\n%s", v)
	}
	// Still in use: never deleted, even though it was asked for.
	if ok, _ := deps.FS.Exists("/etc/letsencrypt/live/shop.example.org"); !ok {
		t.Error("the certificate in use was deleted")
	}

	// Retry on the SSL tab: switch to a Layte certificate and delete the old one.
	deps.FS.WriteFileAtomic("/usr/bin/certbot", certbot, 0o755)
	st, body := c.do("POST", "/api/v1/sites/"+shop+"/ssl", map[string]any{"email": "admin@example.org", "delete_old_cert": true})
	if st != 202 {
		t.Fatalf("switch: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("switch job: %v", job)
	}
	if _, site := c.do("GET", "/api/v1/sites/"+shop, nil); site["cert_path"] != shopLayteCert {
		t.Errorf("after switching: %v", site["cert_path"])
	}
	if ok, _ := deps.FS.Exists("/etc/letsencrypt/live/shop.example.org"); ok {
		t.Error("old certificate not deleted after switching")
	}
}
