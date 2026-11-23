package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/store"
)

const testLoginToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// loginAgent answers wp.login_token and records the params it got.
type loginAgent struct {
	got       *actions.WPLoginParams
	actionURL string
}

func (a loginAgent) Call(_ context.Context, action string, _ agent.Actor, params, out any) error {
	if action != "wp.login_token" {
		return agent.Errorf(agent.CodeInvalidParams, "unexpected %s", action)
	}
	*a.got = params.(actions.WPLoginParams)
	b, _ := json.Marshal(actions.WPLogin{ActionURL: a.actionURL, Token: testLoginToken, UserID: 1})
	return json.Unmarshal(b, out)
}

func getPage(t *testing.T, c *client, path string) (*http.Response, string) {
	t.Helper()
	resp, err := c.http.Get(c.env.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestWPAdminLogin(t *testing.T) {
	var got actions.WPLoginParams
	env := newTestEnvWithAgent(t, loginAgent{got: &got, actionURL: "https://blog.example.com/wp-login.php"})
	ctx := context.Background()
	id, err := env.st.CreateSite(ctx, store.Site{Domain: "blog.example.com", SystemUser: "blog", PHPVersion: "8.3",
		Managed: true, Source: "created", WordPress: true, Status: "active", RootPath: "/home/blog/blog.example.com/public"})
	if err != nil {
		t.Fatal(err)
	}
	c := env.loggedIn(t)
	resp, body := getPage(t, c, "/api/v1/sites/"+strconv.FormatInt(id, 10)+"/wp-admin")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if got.Domain != "blog.example.com" || got.Username != "blog" || got.PHPVersion != "8.3" || got.WebRoot != "" {
		t.Errorf("agent params %+v", got)
	}
	if !strings.Contains(body, `action="https://blog.example.com/wp-login.php"`) || !strings.Contains(body, `value="`+testLoginToken+`"`) ||
		!strings.Contains(body, `method="post"`) || !strings.Contains(body, `src="/api/v1/wp-admin.js"`) {
		t.Errorf("page:\n%s", body)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "form-action https://blog.example.com http://blog.example.com") || !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP %q", csp)
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("headers %v", resp.Header)
	}

	// Not logged in: nothing.
	anon := env.client(t)
	if resp, _ := getPage(t, anon, "/api/v1/sites/"+strconv.FormatInt(id, 10)+"/wp-admin"); resp.StatusCode != 401 {
		t.Errorf("anonymous: %d", resp.StatusCode)
	}
	if resp, body := getPage(t, c, "/api/v1/wp-admin.js"); resp.StatusCode != 200 || !strings.Contains(body, "submit()") {
		t.Errorf("script: %d %s", resp.StatusCode, body)
	}
}

func TestWPAdminRefuses(t *testing.T) {
	var got actions.WPLoginParams
	env := newTestEnvWithAgent(t, loginAgent{got: &got, actionURL: "https://evil.example.net/wp-login.php"})
	ctx := context.Background()
	mk := func(s store.Site) string {
		id, err := env.st.CreateSite(ctx, s)
		if err != nil {
			t.Fatal(err)
		}
		return "/api/v1/sites/" + strconv.FormatInt(id, 10) + "/wp-admin"
	}
	plain := mk(store.Site{Domain: "plain.example.com", SystemUser: "plain", PHPVersion: "8.3", Managed: true, Source: "created", Status: "active"})
	imported := mk(store.Site{Domain: "old.example.com", PHPVersion: "8.3", Source: "imported", WordPress: true, Status: "active"})
	wrongHost := mk(store.Site{Domain: "wp.example.com", SystemUser: "wpsite", PHPVersion: "8.3", Managed: true, Source: "created", WordPress: true, Status: "active"})

	c := env.loggedIn(t)
	for path, want := range map[string]int{plain: 400, imported: 409, wrongHost: 502, "/api/v1/sites/999/wp-admin": 404} {
		resp, body := getPage(t, c, path)
		if resp.StatusCode != want || strings.Contains(body, testLoginToken) {
			t.Errorf("%s: %d, want %d\n%s", path, resp.StatusCode, want, body)
		}
		if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "form-action 'none'") {
			t.Errorf("%s: CSP %q", path, resp.Header.Get("Content-Security-Policy"))
		}
	}
}
