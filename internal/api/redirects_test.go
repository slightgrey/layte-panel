package api

import (
	"strconv"
	"strings"
	"testing"
)

func TestRedirects(t *testing.T) {
	_, c, deps := siteEnv(t)
	id := strconv.Itoa(createSite(t, c, "redir.test", "8.3"))
	path := "/api/v1/sites/" + id + "/redirects"
	vhostPath := "/etc/nginx/sites-available/layte-redir.test.conf"

	for _, bad := range []any{
		map[string]any{"redirects": []map[string]any{{"from": "/a", "to": "/b; return 200"}}},
		map[string]any{"redirects": []map[string]any{{"from": "/a", "to": "/b$request_uri"}}},
		map[string]any{"redirects": []map[string]any{{"from": "/", "to": "/b"}}},
		map[string]any{"redirects": []map[string]any{{"from": "/a", "to": "/b", "code": 307}}},
		map[string]any{"redirects": []map[string]any{}}, // nothing changed
	} {
		if st, body := c.do("PUT", path, bad); st != 400 {
			t.Errorf("%v: %d %v", bad, st, body)
		}
	}

	set := func(list []map[string]any) {
		t.Helper()
		st, body := c.do("PUT", path, map[string]any{"redirects": list})
		if st != 202 {
			t.Fatalf("set: %d %v", st, body)
		}
		if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
			t.Fatalf("job: %v", job)
		}
	}
	set([]map[string]any{{"from": "/hire-vince/", "to": "/hire/"}, {"from": "/old", "to": "https://example.org/new", "code": 302}})
	v, _ := deps.FS.ReadFile(vhostPath)
	for _, want := range []string{"location = /hire-vince { return 301 /hire/; }", "location = /hire-vince/ { return 301 /hire/; }", "location = /old { return 302 https://example.org/new; }"} {
		if !strings.Contains(string(v), want) {
			t.Errorf("vhost lacks %q:\n%s", want, v)
		}
	}
	_, site := c.do("GET", "/api/v1/sites/"+id, nil)
	if r, _ := site["redirects"].([]any); len(r) != 2 || r[0].(map[string]any)["from"] != "/hire-vince" {
		t.Errorf("stored: %v", site["redirects"])
	}

	// Other config changes keep the redirects.
	_, body := c.do("PUT", "/api/v1/sites/"+id+"/php-settings", map[string]any{"upload_max": "128M"})
	waitJob(t, c, body["job_id"].(string))
	if v, _ := deps.FS.ReadFile(vhostPath); !strings.Contains(string(v), "location = /hire-vince ") {
		t.Error("PHP settings dropped the redirects")
	}

	// An empty list removes them.
	set([]map[string]any{})
	if v, _ := deps.FS.ReadFile(vhostPath); strings.Contains(string(v), "Redirects") {
		t.Errorf("redirects left in the vhost:\n%s", v)
	}
	if _, site := c.do("GET", "/api/v1/sites/"+id, nil); len(site["redirects"].([]any)) != 0 {
		t.Errorf("stored after clearing: %v", site["redirects"])
	}
}
