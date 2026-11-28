package api

import (
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/system"
)

func TestServicesAndReboot(t *testing.T) {
	_, c, deps := siteEnv(t)

	code, body := c.do("GET", "/api/v1/system/services", nil)
	if code != 200 {
		t.Fatalf("services: %d %v", code, body)
	}
	var nginxOp string
	for _, s := range body["services"].([]any) {
		if m := s.(map[string]any); m["name"] == "nginx" {
			nginxOp = m["action"].(map[string]any)["op"].(string)
		}
	}
	if nginxOp != "reload" {
		t.Errorf("nginx action = %q", nginxOp)
	}

	// A reload runs the config test first, then systemctl reload.
	if code, body := c.do("POST", "/api/v1/system/services/nginx/reload", nil); code != 200 {
		t.Fatalf("reload nginx: %d %v", code, body)
	}
	calls := strings.Join(deps.Runner.(*system.FakeRunner).CallList(), "\n")
	if !strings.Contains(calls, "nginx -t") || !strings.Contains(calls, "systemctl reload nginx.service") {
		t.Errorf("calls:\n%s", calls)
	}
	for _, bad := range []string{"/api/v1/system/services/nginx/restart", "/api/v1/system/services/layte-agent/restart", "/api/v1/system/services/ufw/reload"} {
		if code, _ := c.do("POST", bad, nil); code != 400 {
			t.Errorf("POST %s: %d, want 400", bad, code)
		}
	}

	// Reboot: needs the hostname, is scheduled a minute out, can be cancelled.
	_, st := c.do("GET", "/api/v1/system/reboot", nil)
	host, _ := st["hostname"].(string)
	if code, _ := c.do("POST", "/api/v1/system/reboot", map[string]string{"confirm": "nope"}); code != 400 {
		t.Errorf("reboot without the right name: %d", code)
	}
	if host == "" {
		t.Skip("the fake root has no hostname")
	}
	code, body = c.do("POST", "/api/v1/system/reboot", map[string]string{"confirm": strings.ToUpper(host)})
	if code != 202 || body["scheduled"] == nil {
		t.Fatalf("reboot: %d %v", code, body)
	}
	if code, body := c.do("DELETE", "/api/v1/system/reboot", nil); code != 200 || body["scheduled"] != nil {
		t.Errorf("cancel: %d %v", code, body)
	}
}
