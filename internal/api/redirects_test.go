package api

import (
	"strconv"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/system"
)

func TestRedirects(t *testing.T) {
	_, c, deps := siteEnv(t)
	id := strconv.Itoa(createSite(t, c, "redir.test", "8.3"))
	path := "/api/v1/sites/" + id + "/redirects"
	vhostPath := "/etc/nginx/sites-available/layte-redir.test.conf"
	redirPath := "/etc/nginx/layte-redirects/redir.test.conf"
	include := "include " + redirPath + ";"

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

	// The vhost only includes the redirects file; the redirects are in it.
	if v, _ := deps.FS.ReadFile(vhostPath); !strings.Contains(string(v), include) || strings.Contains(string(v), "location = /hire-vince") {
		t.Errorf("vhost should only include the redirects file:\n%s", v)
	}
	r, _ := deps.FS.ReadFile(redirPath)
	for _, want := range []string{"# Managed by Layte Panel", "location = /hire-vince { return 301 /hire/; }",
		"location = /hire-vince/ { return 301 /hire/; }", "location = /old { return 302 https://example.org/new; }"} {
		if !strings.Contains(string(r), want) {
			t.Errorf("redirects file lacks %q:\n%s", want, r)
		}
	}
	_, site := c.do("GET", "/api/v1/sites/"+id, nil)
	if l, _ := site["redirects"].([]any); len(l) != 2 || l[0].(map[string]any)["from"] != "/hire-vince" {
		t.Errorf("stored: %v", site["redirects"])
	}

	// Other config changes keep the redirects.
	_, body := c.do("PUT", "/api/v1/sites/"+id+"/php-settings", map[string]any{"upload_max": "128M"})
	waitJob(t, c, body["job_id"].(string))
	if v, _ := deps.FS.ReadFile(vhostPath); !strings.Contains(string(v), include) {
		t.Error("PHP settings dropped the redirects include")
	}
	if ok, _ := deps.FS.Exists(redirPath); !ok {
		t.Error("PHP settings removed the redirects file")
	}

	// An empty list removes the include and the file.
	set([]map[string]any{})
	if v, _ := deps.FS.ReadFile(vhostPath); strings.Contains(string(v), include) {
		t.Errorf("include left in the vhost:\n%s", v)
	}
	if ok, _ := deps.FS.Exists(redirPath); ok {
		t.Error("redirects file left behind")
	}
	if _, site := c.do("GET", "/api/v1/sites/"+id, nil); len(site["redirects"].([]any)) != 0 {
		t.Errorf("stored after clearing: %v", site["redirects"])
	}

	// Deleting the site removes its redirects file too.
	set([]map[string]any{{"from": "/x", "to": "/y"}})
	st, body := c.do("DELETE", "/api/v1/sites/"+id, map[string]any{"confirm_domain": "redir.test"})
	if st != 202 {
		t.Fatalf("delete: %d %v", st, body)
	}
	waitJob(t, c, body["job_id"].(string))
	if ok, _ := deps.FS.Exists(redirPath); ok {
		t.Error("redirects file left after deleting the site")
	}
}

// A failed nginx test leaves the previous redirects in place.
func TestRedirectsRollback(t *testing.T) {
	_, c, deps := siteEnv(t)
	id := strconv.Itoa(createSite(t, c, "redir2.test", "8.3"))
	vhostPath := "/etc/nginx/sites-available/layte-redir2.test.conf"
	redirPath := "/etc/nginx/layte-redirects/redir2.test.conf"
	put := func(to string) map[string]any {
		t.Helper()
		_, body := c.do("PUT", "/api/v1/sites/"+id+"/redirects", map[string]any{"redirects": []map[string]any{{"from": "/a", "to": to}}})
		return waitJob(t, c, body["job_id"].(string))
	}

	// First redirects: the file passes, the vhost (now including it) fails →
	// the new file is removed again and the vhost stays as it was.
	vhost, _ := deps.FS.ReadFile(vhostPath)
	restore := failNginxTest(deps, 2)
	if job := put("/b"); job["status"] != "failed" {
		t.Fatalf("job should fail: %v", job)
	}
	restore()
	if ok, _ := deps.FS.Exists(redirPath); ok {
		t.Error("redirects file left after the vhost failed")
	}
	if v, _ := deps.FS.ReadFile(vhostPath); string(v) != string(vhost) {
		t.Error("vhost changed")
	}

	// Changing existing redirects: only the file changes; a failed test keeps the old one.
	if job := put("/b"); job["status"] != "succeeded" {
		t.Fatalf("job: %v", job)
	}
	before, _ := deps.FS.ReadFile(redirPath)
	restore = failNginxTest(deps, 1)
	if job := put("/c"); job["status"] != "failed" {
		t.Fatalf("job should fail: %v", job)
	}
	restore()
	if after, _ := deps.FS.ReadFile(redirPath); string(after) != string(before) {
		t.Errorf("redirects file changed after a failed test:\n%s", after)
	}
	if _, site := c.do("GET", "/api/v1/sites/"+id, nil); site["redirects"].([]any)[0].(map[string]any)["to"] != "/b" {
		t.Errorf("stored: %v", site["redirects"])
	}
}

// failNginxTest makes the nth `nginx -t` from now fail; call the result to undo.
func failNginxTest(deps actions.Deps, nth int) func() {
	fr := deps.Runner.(*system.FakeRunner)
	orig := fr.Handle
	n := 0
	fr.Handle = func(cmd system.Cmd) (system.Result, error) {
		if cmd.String() == "/usr/sbin/nginx -t" {
			if n++; n == nth {
				return system.Result{Stderr: []byte("nginx: [emerg] boom")}, &system.ExitError{Bin: cmd.Bin, ExitCode: 1}
			}
		}
		return orig(cmd)
	}
	return func() { fr.Handle = orig }
}
