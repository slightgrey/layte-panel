package api

import (
	"strconv"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/system"
)

func TestSSLLifecycle(t *testing.T) {
	_, c, deps := siteEnv(t)
	c.do("POST", "/api/v1/sites/scan", nil)
	_, body := c.do("POST", "/api/v1/sites", map[string]any{"domain": "secure.test", "aliases": []string{"www.secure.test"}, "php_version": "8.3",
		"wordpress": map[string]string{"title": "Secure", "admin_user": "owner", "admin_email": "o@example.com"}})
	waitJob(t, c, body["job_id"].(string))
	id := strconv.Itoa(int(body["site_id"].(float64)))

	if st, _ := c.do("POST", "/api/v1/sites/"+id+"/ssl", map[string]string{"email": "nope"}); st != 400 {
		t.Errorf("bad email: %d", st)
	}
	st, body := c.do("POST", "/api/v1/sites/"+id+"/ssl", map[string]string{"email": "admin@example.com"})
	if st != 202 {
		t.Fatalf("issue: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("issue job: %v", job)
	}

	vhost, _ := deps.FS.ReadFile("/etc/nginx/sites-available/layte-secure.test.conf")
	for _, want := range []string{"listen 443 ssl http2;", "/etc/letsencrypt/live/layte-secure.test/fullchain.pem", "return 301 https://$host$request_uri;"} {
		if !strings.Contains(string(vhost), want) {
			t.Errorf("vhost missing %q", want)
		}
	}
	if !exists(t, deps.FS, "/etc/letsencrypt/renewal-hooks/deploy/layte-reload-nginx") {
		t.Error("renewal deploy hook not installed")
	}
	var certbotCall, wpURL string
	for _, call := range deps.Runner.(*system.FakeRunner).CallList() {
		if strings.HasPrefix(call, "/usr/bin/certbot certonly") {
			certbotCall = call
		}
		if strings.Contains(call, "option update home") {
			wpURL = call
		}
	}
	if !strings.Contains(certbotCall, "--webroot") || strings.Contains(certbotCall, "--nginx") ||
		!strings.Contains(certbotCall, "--domains secure.test,www.secure.test") {
		t.Errorf("certbot call: %s", certbotCall)
	}
	if !strings.HasSuffix(wpURL, "https://secure.test") {
		t.Errorf("WordPress URL not switched: %q", wpURL)
	}

	// Certificates overview: ours is fine and linked; the imported blog's expires soon.
	_, list := c.do("GET", "/api/v1/ssl", nil)
	if list["email"] != "admin@example.com" {
		t.Errorf("email not remembered: %v", list["email"])
	}
	states := map[string]map[string]any{}
	for _, x := range list["certificates"].([]any) {
		cert := x.(map[string]any)
		states[cert["name"].(string)] = cert
	}
	if c := states["layte-secure.test"]; c["state"] != "ok" || c["managed"] != true || c["site_domain"] != "secure.test" {
		t.Errorf("our cert: %v", c)
	}
	if c := states["blog.example.com"]; c["state"] != "expiring" || c["managed"] != false || c["site_domain"] != "blog.example.com" {
		t.Errorf("blog cert: %v", c)
	}

	// Switching PHP keeps HTTPS.
	_, body = c.do("POST", "/api/v1/sites/"+id+"/php", map[string]string{"version": "8.2"})
	waitJob(t, c, body["job_id"].(string))
	vhost, _ = deps.FS.ReadFile("/etc/nginx/sites-available/layte-secure.test.conf")
	if !strings.Contains(string(vhost), "listen 443 ssl") || !strings.Contains(string(vhost), "php8.2.sock") {
		t.Error("PHP switch dropped HTTPS")
	}

	// Remove HTTPS: vhost back to HTTP, certificate revoked.
	_, body = c.do("DELETE", "/api/v1/sites/"+id+"/ssl", nil)
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("remove job: %v", job)
	}
	vhost, _ = deps.FS.ReadFile("/etc/nginx/sites-available/layte-secure.test.conf")
	if strings.Contains(string(vhost), "443") || exists(t, deps.FS, "/etc/letsencrypt/live/layte-secure.test") {
		t.Error("HTTPS not removed")
	}
	_, site := c.do("GET", "/api/v1/sites/"+id, nil)
	if site["ssl"] != false {
		t.Errorf("site still marked ssl: %v", site)
	}
}

func TestSSLRefusesImportedSites(t *testing.T) {
	_, c, _ := siteEnv(t)
	_, scan := c.do("POST", "/api/v1/sites/scan", nil)
	id := strconv.Itoa(int(scan["sites"].([]any)[0].(map[string]any)["id"].(float64)))
	if st, body := c.do("POST", "/api/v1/sites/"+id+"/ssl", map[string]string{"email": "a@example.com"}); st != 409 {
		t.Errorf("%d %v", st, body)
	}
}

func TestVhostNeedsCertForSSL(t *testing.T) {
	_, c, deps := siteEnv(t)
	_, body := c.do("POST", "/api/v1/sites", map[string]any{"domain": "nocert.test", "php_version": "8.3"})
	waitJob(t, c, body["job_id"].(string))
	// Make certbot "succeed" without producing files: the vhost step must refuse.
	fr := deps.Runner.(*system.FakeRunner)
	orig := fr.Handle
	fr.Handle = func(cmd system.Cmd) (system.Result, error) {
		if strings.HasPrefix(cmd.String(), "/usr/bin/certbot certonly") {
			return system.Result{}, nil
		}
		return orig(cmd)
	}
	_, body2 := c.do("POST", "/api/v1/sites/"+strconv.Itoa(int(body["site_id"].(float64)))+"/ssl", map[string]string{"email": "a@example.com"})
	job := waitJob(t, c, body2["job_id"].(string))
	if job["status"] != "failed" || !strings.Contains(job["error"].(string), "no certificate") {
		t.Errorf("job: %v", job)
	}
	vhost, _ := deps.FS.ReadFile("/etc/nginx/sites-available/layte-nocert.test.conf")
	if strings.Contains(string(vhost), "443") {
		t.Error("vhost switched to HTTPS without a certificate")
	}
}
