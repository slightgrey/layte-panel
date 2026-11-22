package api

import (
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/system"
)

func TestAdoptSite(t *testing.T) {
	_, c, deps := siteEnv(t)
	c.do("POST", "/api/v1/sites/scan", nil)
	shop := siteIDByDomain(t, c, "shop.example.org")

	st, prev := c.do("GET", "/api/v1/sites/"+shop+"/adopt", nil)
	if st != 200 || prev["username"] != "shopexampleorg" || prev["old_user"] != "shop" || prev["pool_hash"] == "" {
		t.Fatalf("preview: %d %v", st, prev)
	}
	if st, body := c.do("POST", "/api/v1/sites/"+shop+"/adopt", map[string]string{"username": "Bad Name", "pool_hash": prev["pool_hash"].(string)}); st != 400 {
		t.Errorf("bad username: %d %v", st, body)
	}
	st, body := c.do("POST", "/api/v1/sites/"+shop+"/adopt", map[string]string{"username": "shopexampleorg", "pool_hash": prev["pool_hash"].(string)})
	if st != 202 {
		t.Fatalf("adopt: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("adopt job: %v", job)
	}
	_, site := c.do("GET", "/api/v1/sites/"+shop, nil)
	if site["adopted"] != true || site["managed"] != false || site["system_user"] != "shopexampleorg" ||
		!strings.HasSuffix(site["fpm_pool"].(string), "/8.2/fpm/pool.d/shop.conf") {
		t.Errorf("site after adopt: %v", site)
	}
	// A rescan keeps it adopted; adopting twice is refused.
	c.do("POST", "/api/v1/sites/scan", nil)
	if _, site := c.do("GET", "/api/v1/sites/"+shop, nil); site["adopted"] != true || site["system_user"] != "shopexampleorg" {
		t.Errorf("after rescan: %v", site)
	}
	if st, body := c.do("GET", "/api/v1/sites/"+shop+"/adopt", nil); st != 409 || errCode(body) != "site_adopted" {
		t.Errorf("preview again: %d %v", st, body)
	}
	// Files: writable now (web root), plus the data area for dumps and logs.
	if st, body := c.do("POST", "/api/v1/sites/"+shop+"/files/mkdir", map[string]string{"path": "uploads"}); st != 204 {
		t.Errorf("mkdir on adopted site: %d %v", st, body)
	}
	if st, body := c.do("GET", "/api/v1/sites/"+shop+"/files?path=tmp&area=data", nil); st != 200 || body["read_only"] == true {
		t.Errorf("data area: %d %v", st, body)
	}
	// Cron: allowed, runs as the new user from the web root.
	if st, body := c.do("POST", "/api/v1/sites/"+shop+"/cron", map[string]string{"schedule": "@hourly", "command": "php cron.php"}); st != 201 {
		t.Errorf("add cron: %d %v", st, body)
	}
	if b, _ := deps.FS.ReadFile("/etc/cron.d/layte-shop_example_org"); !strings.Contains(string(b), "shopexampleorg cd /var/www/shop/public &&") {
		t.Errorf("cron file:\n%s", b)
	}

	// The vhost was not touched.
	if b, _ := deps.FS.ReadFile("/etc/nginx/sites-enabled/shop.example.org.conf"); strings.Contains(string(b), "Layte") {
		t.Error("vhost changed")
	}

	// The blog uses the default www pool: the preview says why, the job refuses.
	blog := siteIDByDomain(t, c, "blog.example.com")
	if st, _ := c.do("POST", "/api/v1/sites/"+blog+"/files/mkdir", map[string]string{"path": "x"}); st != 409 {
		t.Errorf("imported site writable: %d", st)
	}
	if st, _ := c.do("POST", "/api/v1/sites/"+blog+"/cron", map[string]string{"schedule": "@hourly", "command": "id"}); st != 409 {
		t.Errorf("cron on imported site: %d", st)
	}
	_, prev = c.do("GET", "/api/v1/sites/"+blog+"/adopt", nil)
	if b, _ := prev["blockers"].([]any); len(b) == 0 {
		t.Fatalf("blog preview: %v", prev)
	}
	_, body = c.do("POST", "/api/v1/sites/"+blog+"/adopt", map[string]string{"username": "blogexamplecom", "pool_hash": prev["pool_hash"].(string)})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "failed" {
		t.Errorf("blog adopt should fail: %v", job)
	}
	if _, site := c.do("GET", "/api/v1/sites/"+blog, nil); site["adopted"] != false {
		t.Error("blog marked adopted")
	}
}

func TestAdoptedSiteDatabase(t *testing.T) {
	_, c, deps := siteEnv(t)
	// Make shop a WordPress site whose wp-config.php uses the "blog" database.
	deps.FS.WriteFileAtomic("/var/www/shop/public/wp-config.php", []byte("<?php\ndefine( 'DB_NAME', 'blog' );\ndefine( 'DB_USER', 'bloguser' );\n"), 0o640)
	deps.FS.MkdirAll("/var/lib/mysql/otherdb", 0o750)
	c.do("POST", "/api/v1/sites/scan", nil)
	shop := siteIDByDomain(t, c, "shop.example.org")
	_, prev := c.do("GET", "/api/v1/sites/"+shop+"/adopt", nil)
	_, body := c.do("POST", "/api/v1/sites/"+shop+"/adopt", map[string]string{"username": "shopexampleorg", "pool_hash": prev["pool_hash"].(string)})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("adopt: %v", job)
	}

	// Only adopted sites link existing databases; system databases never.
	blogSite := siteIDByDomain(t, c, "blog.example.com")
	if st, body := c.do("POST", "/api/v1/sites/"+blogSite+"/databases/link", map[string]string{"name": "blog"}); st != 409 {
		t.Errorf("link to imported site: %d %v", st, body)
	}
	if st, _ := c.do("POST", "/api/v1/sites/"+shop+"/databases/link", map[string]string{"name": "mysql"}); st != 400 {
		t.Errorf("link system db: %d", st)
	}
	if st, _ := c.do("POST", "/api/v1/sites/"+shop+"/databases/link", map[string]string{"name": "nosuchdb"}); st != 404 {
		t.Errorf("link missing db: %d", st)
	}
	// wp-config.php names another database: refused.
	if st, body := c.do("POST", "/api/v1/sites/"+shop+"/databases/link", map[string]string{"name": "otherdb"}); st != 409 || errCode(body) != "db_mismatch" {
		t.Errorf("link db wp-config doesn't use: %d %v", st, body)
	}
	if st, body := c.do("POST", "/api/v1/sites/"+shop+"/databases/link", map[string]string{"name": "blog"}); st != 204 {
		t.Fatalf("link: %d %v", st, body)
	}
	if d := dbByName(t, c)["blog"]; d["linked"] != true || d["site_domain"] != "shop.example.org" || d["user"] != "bloguser" {
		t.Errorf("linked db (user from wp-config.php): %v", d)
	}
	// Linked databases are never dropped or have their password reset.
	if st, body := c.do("POST", "/api/v1/databases/blog/password", nil); st != 409 || errCode(body) != "db_linked" {
		t.Errorf("reset linked: %d %v", st, body)
	}
	if st, body := c.do("DELETE", "/api/v1/databases/blog", map[string]string{"confirm": "blog"}); st != 409 || errCode(body) != "db_linked" {
		t.Errorf("drop linked: %d %v", st, body)
	}

	// Export lands in /home/<user>/tmp (outside the web root); import reads it back.
	_, body = c.do("POST", "/api/v1/databases/blog/export", nil)
	job := waitJob(t, c, body["job_id"].(string))
	if job["status"] != "succeeded" {
		t.Fatalf("export: %v", job)
	}
	rel := job["result"].(map[string]any)["path"].(string)
	if !exists(t, deps.FS, "/home/shopexampleorg/"+rel) {
		t.Fatalf("dump not in the data folder: %s", rel)
	}
	if st, body := c.do("GET", "/api/v1/sites/"+shop+"/files?path=tmp&area=data", nil); st != 200 || len(body["entries"].([]any)) != 1 {
		t.Errorf("dump not listed in the data area: %d %v", st, body)
	}
	_, body = c.do("POST", "/api/v1/databases/blog/import", map[string]string{"path": rel, "confirm": "blog"})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Errorf("import: %v", job)
	}

	// Unlink: Layte forgets it, the database stays.
	if st, _ := c.do("DELETE", "/api/v1/databases/blog/link", nil); st != 204 {
		t.Errorf("unlink: %d", st)
	}
	if d, ok := dbByName(t, c)["blog"]; !ok || d["managed"] != false {
		t.Errorf("after unlink: %v", d)
	}
}

func TestAdoptedSiteRenewsItsCertificate(t *testing.T) {
	_, c, deps := siteEnv(t)
	// Give the hand-written shop vhost an HTTPS block with a certbot
	// certificate (like the live blog), then scan.
	vhost := "/etc/nginx/sites-enabled/shop.example.org.conf"
	b, _ := deps.FS.ReadFile(vhost)
	https := "\nserver {\n    listen 443 ssl;\n    server_name shop.example.org;\n    root /var/www/shop/public;\n" +
		"    ssl_certificate /etc/letsencrypt/live/shop.example.org/fullchain.pem;\n" +
		"    location ~ \\.php$ { fastcgi_pass unix:/run/php/shop.sock; }\n}\n"
	deps.FS.WriteFileAtomic(vhost, append(b, https...), 0o644)
	deps.FS.MkdirAll("/etc/letsencrypt/renewal", 0o755)
	deps.FS.WriteFileAtomic("/etc/letsencrypt/renewal/shop.example.org.conf", []byte("# test\n"), 0o644)
	c.do("POST", "/api/v1/sites/scan", nil)
	shop := siteIDByDomain(t, c, "shop.example.org")

	// Not adopted yet: refused.
	if st, _ := c.do("POST", "/api/v1/sites/"+shop+"/ssl/renew", map[string]bool{"dry_run": true}); st != 409 {
		t.Errorf("renew before adopting: %d", st)
	}
	_, prev := c.do("GET", "/api/v1/sites/"+shop+"/adopt", nil)
	_, body := c.do("POST", "/api/v1/sites/"+shop+"/adopt", map[string]string{"username": "shopexampleorg", "pool_hash": prev["pool_hash"].(string)})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("adopt: %v", job)
	}
	st, body := c.do("POST", "/api/v1/sites/"+shop+"/ssl/renew", map[string]bool{"dry_run": true})
	if st != 202 {
		t.Fatalf("renew: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("renew job: %v", job)
	}
	var call string
	for _, x := range deps.Runner.(*system.FakeRunner).CallList() {
		if strings.HasPrefix(x, "/usr/bin/certbot renew") {
			call = x
		}
	}
	if !strings.Contains(call, "--cert-name shop.example.org ") || !strings.HasSuffix(call, "--dry-run") {
		t.Errorf("certbot call: %q", call)
	}
}
