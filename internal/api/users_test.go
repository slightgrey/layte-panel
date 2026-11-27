package api

import (
	"context"
	"fmt"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/auth"
)

func TestSiteAccountLifecycle(t *testing.T) {
	ag, _ := devAgent(t) // applying a login's limits talks to the agent
	e := newTestEnvWithAgent(t, ag)
	main, sub, _ := accountSites(t, e)
	a := e.loggedIn(t)

	// Only main sites get a login.
	if st, body := a.do("POST", "/api/v1/users", map[string]any{"username": "client", "site_id": sub}); st != 400 {
		t.Errorf("login for a subdomain site: %d %v", st, body)
	}
	st, body := a.do("POST", "/api/v1/users", map[string]any{"username": "client", "site_id": main, "max_subdomains": 2})
	if st != 201 {
		t.Fatalf("create: %d %v", st, body)
	}
	if _, ok := body["password"]; ok {
		t.Fatal("a password was generated and shown to the admin")
	}
	token := path.Base(body["setup_url"].(string))
	const pw = "the client's own passphrase"
	id := int64(body["user"].(map[string]any)["id"].(float64))
	if st, _ := a.do("POST", "/api/v1/users", map[string]any{"username": "client2", "site_id": main}); st != 409 {
		t.Errorf("second login for the same site: %d", st)
	}

	// First login: 2FA setup in the browser. (Few auth calls: they are rate
	// limited per IP.)
	c := e.client(t)
	st, body = c.do("POST", "/api/v1/setup/"+token, map[string]string{"password": pw})
	if st != 200 || body["totp_setup"] != true {
		t.Fatalf("setup link: %d %v", st, body)
	}
	if st, body := c.do("POST", "/api/v1/auth/totp", map[string]string{"code": "123456"}); st != 401 || errCode(body) != "totp_setup_required" {
		t.Errorf("totp before setup: %d %v", st, body)
	}
	st, body = c.do("POST", "/api/v1/auth/totp/setup", nil)
	if st != 200 || !strings.HasPrefix(body["uri"].(string), "otpauth://") {
		t.Fatalf("setup: %d %v", st, body)
	}
	c.secret = body["secret"].(string)
	st, body = c.do("POST", "/api/v1/auth/totp/setup/confirm", map[string]string{"code": c.code(0)})
	if st != 200 || body["user"].(map[string]any)["role"] != "site" {
		t.Fatalf("confirm: %d %v", st, body)
	}
	if st, _ := c.do("GET", "/api/v1/auth/me", nil); st != 200 {
		t.Errorf("me after setup: %d", st)
	}
	// The secret can't be replaced by running the setup again.
	c2 := e.client(t)
	c2.do("POST", "/api/v1/auth/login", map[string]string{"username": "client", "password": pw})
	if st, _ := c2.do("POST", "/api/v1/auth/totp/setup", nil); st != 409 {
		t.Errorf("setup with 2FA already set up: %d", st)
	}
	// Normal 2FA from now on (a later step than the setup code).
	c2.secret = c.secret
	if st, body := c2.do("POST", "/api/v1/auth/totp", map[string]string{"code": c2.code(30e9)}); st != 200 {
		t.Errorf("second login: %d %v", st, body)
	}

	// Admin: limit, password and 2FA resets, delete. Site accounts can't.
	upath := fmt.Sprintf("/api/v1/users/%d", id)
	if st, _ := c.do("GET", "/api/v1/users", nil); st != 403 {
		t.Errorf("site account lists users: %d", st)
	}
	if st, body := a.do("PATCH", upath, map[string]int{"max_subdomains": 7}); st != 200 || body["max_subdomains"] != float64(7) {
		t.Errorf("patch: %d %v", st, body)
	}
	if st, _ := a.do("PATCH", upath, map[string]int{"max_subdomains": 1000}); st != 400 {
		t.Errorf("patch over the cap: %d", st)
	}
	st, body = a.do("POST", upath+"/password", nil)
	if st != 200 || !strings.Contains(fmt.Sprint(body["setup_url"]), "/setup/") || body["password"] != nil {
		t.Errorf("password reset link: %d %v", st, body)
	}
	if st, _ := c.do("GET", "/api/v1/auth/me", nil); st != 200 {
		t.Errorf("a reset link logged the user out before it was used: %d", st)
	}
	if st, _ := a.do("POST", upath+"/reset-2fa", nil); st != 204 {
		t.Errorf("reset 2fa: %d", st)
	}
	if u, _ := e.st.UserByID(context.Background(), id); len(u.TOTPSecretEnc) != 0 {
		t.Error("2FA secret kept after reset")
	}
	st, body = a.do("GET", "/api/v1/users", nil)
	if st != 200 || len(body["users"].([]any)) != 2 {
		t.Errorf("list: %d %v", st, body)
	}
	admin, _ := e.st.UserByUsername(context.Background(), "admin")
	if st, _ := a.do("DELETE", fmt.Sprintf("/api/v1/users/%d", admin.ID), nil); st != 409 {
		t.Errorf("delete an admin from the UI: %d", st)
	}
	if st, _ := a.do("DELETE", upath, nil); st != 204 {
		t.Errorf("delete: %d", st)
	}
	if _, err := e.st.SiteByID(context.Background(), main); err != nil {
		t.Errorf("site went with the login: %v", err)
	}
}

// An admin without 2FA still has to use the CLI (no browser setup).
func TestAdminWithoutTOTPCantSetUpInBrowser(t *testing.T) {
	e := newTestEnv(t)
	hash, _ := auth.HashPassword(testPassword)
	if _, err := e.st.CreateUser(context.Background(), "bob", hash, nil); err != nil {
		t.Fatal(err)
	}
	c := e.client(t)
	if st, body := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": testPassword}); st != 403 || errCode(body) != "totp_not_enrolled" {
		t.Errorf("login: %d %v", st, body)
	}
}

// Setup links work once, expire, and a new link cancels the old one.
func TestSetupLinks(t *testing.T) {
	ag, _ := devAgent(t)
	e := newTestEnvWithAgent(t, ag)
	main, _, _ := accountSites(t, e)
	a := e.loggedIn(t)
	_, body := a.do("POST", "/api/v1/users", map[string]any{"username": "client", "site_id": main})
	first := path.Base(body["setup_url"].(string))
	id := int64(body["user"].(map[string]any)["id"].(float64))

	// A reset link replaces the first one.
	_, body = a.do("POST", fmt.Sprintf("/api/v1/users/%d/password", id), nil)
	second := path.Base(body["setup_url"].(string))
	c := e.client(t)
	if st, _ := c.do("GET", "/api/v1/setup/"+first, nil); st != 404 {
		t.Errorf("replaced link still works: %d", st)
	}
	if st, body := c.do("GET", "/api/v1/setup/"+second, nil); st != 200 || body["username"] != "client" {
		t.Errorf("link info: %d %v", st, body)
	}
	if st, _ := c.do("POST", "/api/v1/setup/"+second, map[string]string{"password": "short"}); st != 400 {
		t.Errorf("short password: %d", st)
	}
	if st, _ := c.do("POST", "/api/v1/setup/"+second, map[string]string{"password": "a long enough passphrase"}); st != 200 {
		t.Fatalf("use link: %d", st)
	}
	if st, _ := c.do("POST", "/api/v1/setup/"+second, map[string]string{"password": "another long passphrase"}); st != 404 {
		t.Errorf("link used twice: %d", st)
	}

	// Expired after 72 hours.
	_, body = a.do("POST", fmt.Sprintf("/api/v1/users/%d/password", id), nil)
	third := path.Base(body["setup_url"].(string))
	e.now = e.now.Add(73 * time.Hour)
	if st, _ := e.client(t).do("GET", "/api/v1/setup/"+third, nil); st != 404 {
		t.Errorf("expired link works: %d", st)
	}
}
