package api

import (
	"strconv"
	"testing"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

// devAgent returns an in-process agent running the real actions against a
// fresh fake root.
func devAgent(t *testing.T) (*agent.LocalClient, actions.Deps) {
	t.Helper()
	deps, err := actions.DevDeps(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { deps.FS.(*system.RootFS).Close() })
	reg := agent.NewRegistry()
	actions.Register(reg, deps)
	return &agent.LocalClient{Registry: reg}, deps
}

func TestScanSites(t *testing.T) {
	ag, deps := devAgent(t)
	env := newTestEnvWithAgent(t, ag)
	c := env.loggedIn(t)

	st, body := c.do("POST", "/api/v1/sites/scan", nil)
	if st != 200 || body["found"] != float64(3) || body["added"] != float64(3) {
		t.Fatalf("scan: %d %v", st, body)
	}
	sites := body["sites"].([]any)
	first := sites[0].(map[string]any)
	if first["domain"] != "blog.example.com" || first["managed"] != false || first["source"] != "imported" || first["wordpress"] != true {
		t.Errorf("first site = %v", first)
	}

	// Rescan after a vhost disappears: the imported row is removed.
	deps.FS.Remove("/etc/nginx/sites-enabled/static.example.net")
	st, body = c.do("POST", "/api/v1/sites/scan", nil)
	if st != 200 || body["removed"] != float64(1) || len(body["sites"].([]any)) != 2 {
		t.Fatalf("rescan: %d %v", st, body)
	}

	// Detail + vhost viewer.
	id := int(first["id"].(float64))
	if st, body := c.do("GET", "/api/v1/sites/"+strconv.Itoa(id), nil); st != 200 || body["domain"] != "blog.example.com" {
		t.Errorf("get site: %d %v", st, body)
	}
	st, body = c.do("GET", "/api/v1/sites/"+strconv.Itoa(id)+"/vhost", nil)
	if st != 200 || body["managed"] != false || body["content"] == "" {
		t.Errorf("vhost: %d %v", st, body)
	}
	if st, _ := c.do("GET", "/api/v1/sites/999", nil); st != 404 {
		t.Errorf("missing site: %d", st)
	}

	st, body = c.do("GET", "/api/v1/php/versions", nil)
	if v := body["versions"].([]any); st != 200 || len(v) != 2 || v[0] != "8.2" {
		t.Errorf("php versions: %d %v", st, body)
	}
}
