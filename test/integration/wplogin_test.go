//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/store"
)

// postLogin posts a one-time token to the site's wp-login.php (as the panel's
// hand-off page does) and returns the response without following redirects.
func postLogin(t *testing.T, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", "http://127.0.0.1/wp-login.php", strings.NewReader(url.Values{"layte_login": {token}}.Encode()))
	req.Host = domain
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func testWPLogin(t *testing.T, e *env, site store.Site) {
	login, err := e.svc.WPLogin(context.Background(), e.actor, site)
	if err != nil {
		t.Fatal(err)
	}
	if login.ActionURL != "http://"+domain+"/wp-login.php" {
		t.Errorf("action URL %q", login.ActionURL)
	}

	// The token signs in: a redirect to wp-admin with WordPress's login cookie.
	resp := postLogin(t, login.Token)
	loggedIn := false
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "wordpress_logged_in_") {
			loggedIn = true
		}
	}
	if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "/wp-admin") || !loggedIn {
		t.Errorf("login: %s Location=%q logged_in=%v", resp.Status, resp.Header.Get("Location"), loggedIn)
	}

	// Single use.
	if resp := postLogin(t, login.Token); resp.StatusCode != http.StatusForbidden {
		t.Errorf("reused token: %s", resp.Status)
	}
	// A made-up token is refused too.
	if resp := postLogin(t, strings.Repeat("ab", 32)); resp.StatusCode != http.StatusForbidden {
		t.Errorf("unknown token: %s", resp.Status)
	}
}
