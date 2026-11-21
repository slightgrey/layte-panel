package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/config"
)

// fakeAgent answers dashboard actions; actions listed in fail return that error.
type fakeAgent struct {
	fail map[string]error
}

func (f fakeAgent) Call(_ context.Context, action string, actor agent.Actor, _, out any) error {
	if err := f.fail[action]; err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]string{"action": action, "user": actor.Username})
	return json.Unmarshal(b, out)
}

func TestDashboard(t *testing.T) {
	env := newTestEnvWithAgent(t, fakeAgent{fail: map[string]error{
		"updates.pending": agent.Errorf(agent.CodeTimeout, "apt hung"),
		"versions.get":    errors.New("connect: no such file"),
	}})
	c := env.client(t)

	if st, _ := c.do("GET", "/api/v1/dashboard", nil); st != 401 {
		t.Fatalf("unauthenticated dashboard: %d", st)
	}
	c.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	c.do("POST", "/api/v1/auth/totp", map[string]string{"code": c.code(0)})

	st, body := c.do("GET", "/api/v1/dashboard", nil)
	if st != 200 {
		t.Fatalf("dashboard: %d %v", st, body)
	}
	sys := body["system"].(map[string]any)["data"].(map[string]any)
	if sys["action"] != "system.info" || sys["user"] != "admin" {
		t.Errorf("system section: %v", sys)
	}
	if code := body["updates"].(map[string]any)["error"].(map[string]any)["code"]; code != "timeout" {
		t.Errorf("updates error code = %v", code)
	}
	if code := body["versions"].(map[string]any)["error"].(map[string]any)["code"]; code != "agent_unavailable" {
		t.Errorf("versions error code = %v", code)
	}
	// Internal error details must not reach the browser.
	b, _ := json.Marshal(body)
	if strings.Contains(string(b), "apt hung") || strings.Contains(string(b), "no such file") {
		t.Errorf("leaked internal error: %s", b)
	}
}

func TestSPA(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":       {Data: []byte("<html>app</html>")},
		"assets/app-1a.js": {Data: []byte("console.log(1)")},
	}
	h := New(Deps{Config: config.DevDefaults(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), UI: ui})

	tests := []struct {
		path, wantBody, wantCache string
		wantStatus                int
	}{
		{"/", "app", "no-cache", 200},
		{"/sites/example.com", "app", "no-cache", 200}, // client-side route
		{"/assets/app-1a.js", "console.log", "immutable", 200},
		{"/api/v1/nope", "not_found", "", 404},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tt.path, nil))
		if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantBody) ||
			!strings.Contains(rec.Header().Get("Cache-Control"), tt.wantCache) {
			t.Errorf("%s: %d %q cache=%q", tt.path, rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
		}
	}

	rec := httptest.NewRecorder()
	New(Deps{Config: config.DevDefaults(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), UI: fstest.MapFS{}}).
		ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("missing UI: %d", rec.Code)
	}
}
