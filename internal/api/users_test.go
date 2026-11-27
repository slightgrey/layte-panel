package api

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/auth"
)

func TestSiteAccountLifecycle(t *testing.T) {
	e := newTestEnv(t)
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
	pw := body["password"].(string)
	id := int64(body["user"].(map[string]any)["id"].(float64))
	if st, _ := a.do("POST", "/api/v1/users", map[string]any{"username": "client2", "site_id": main}); st != 409 {
		t.Errorf("second login for the same site: %d", st)
	}

	// First login: 2FA setup in the browser. (Few auth calls: they are rate
	// limited per IP.)
	c := e.client(t)
	st, body = c.do("POST", "/api/v1/auth/login", map[string]string{"username": "client", "password": pw})
	if st != 200 || body["totp_setup"] != true {
		t.Fatalf("login: %d %v", st, body)
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
	path := fmt.Sprintf("/api/v1/users/%d", id)
	if st, _ := c.do("GET", "/api/v1/users", nil); st != 403 {
		t.Errorf("site account lists users: %d", st)
	}
	if st, body := a.do("PATCH", path, map[string]int{"max_subdomains": 7}); st != 200 || body["max_subdomains"] != float64(7) {
		t.Errorf("patch: %d %v", st, body)
	}
	if st, _ := a.do("PATCH", path, map[string]int{"max_subdomains": 1000}); st != 400 {
		t.Errorf("patch over the cap: %d", st)
	}
	st, body = a.do("POST", path+"/password", nil)
	if st != 200 || body["password"] == pw {
		t.Errorf("password reset: %d %v", st, body)
	}
	if st, _ := c.do("GET", "/api/v1/auth/me", nil); st != 401 {
		t.Errorf("session survived a password reset: %d", st)
	}
	if st, _ := a.do("POST", path+"/reset-2fa", nil); st != 204 {
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
	if st, _ := a.do("DELETE", path, nil); st != 204 {
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
