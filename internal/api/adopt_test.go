package api

import (
	"strings"
	"testing"
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
	// The vhost was not touched.
	if b, _ := deps.FS.ReadFile("/etc/nginx/sites-enabled/shop.example.org.conf"); strings.Contains(string(b), "Layte") {
		t.Error("vhost changed")
	}

	// The blog uses the default www pool: the preview says why, the job refuses.
	blog := siteIDByDomain(t, c, "blog.example.com")
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
