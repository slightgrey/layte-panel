package api

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/auth"
	"github.com/slightgrey/layte-panel/internal/store"
)

// accountSites creates a site account's main site and subdomain, plus a
// stranger's site. It returns their ids.
func accountSites(t *testing.T, e *testEnv) (main, sub, other int64) {
	t.Helper()
	ctx := context.Background()
	mk := func(domain, user string, parent int64) int64 {
		id, err := e.st.CreateSite(ctx, store.Site{Domain: domain, SystemUser: user, Managed: true, Source: "created", Status: "active", ParentSiteID: parent})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	main = mk("example.com", "example", 0)
	sub = mk("blog.example.com", "example", main)
	other = mk("other.com", "other", 0)
	return
}

// siteUser creates a site account for siteID with 2FA set up and returns a
// logged-in client for it.
func (e *testEnv) siteUser(t *testing.T, username string, siteID int64) *client {
	t.Helper()
	ctx := context.Background()
	hash, _ := auth.HashPassword(testPassword)
	if _, err := e.st.CreateSiteUser(ctx, username, hash, siteID, 5); err != nil {
		t.Fatal(err)
	}
	return e.signIn(t, username)
}

// signIn gives an existing site login the test password and a 2FA secret,
// and returns a logged-in client for it.
func (e *testEnv) signIn(t *testing.T, username string) *client {
	t.Helper()
	ctx := context.Background()
	u, err := e.st.UserByUsername(ctx, username)
	if err != nil {
		t.Fatal(err)
	}
	id := u.ID
	hash, _ := auth.HashPassword(testPassword)
	if err := e.st.SetPassword(ctx, id, hash); err != nil {
		t.Fatal(err)
	}
	enr, _ := auth.NewTOTP(username)
	enc, _ := e.kr.Seal([]byte(enr.Secret), []byte(username))
	if err := e.st.SetTOTPSecret(ctx, id, enc); err != nil {
		t.Fatal(err)
	}
	c := e.client(t)
	c.secret = enr.Secret
	if st, body := c.do("POST", "/api/v1/auth/login", map[string]string{"username": username, "password": testPassword}); st != 200 {
		t.Fatalf("login: %d %v", st, body)
	}
	if st, body := c.do("POST", "/api/v1/auth/totp", map[string]string{"code": c.code(0)}); st != 200 {
		t.Fatalf("totp: %d %v", st, body)
	}
	return c
}

// siteAccountRoutes is every route a site account may reach (with the
// wrapper that limits it). Opening another route to site accounts must be a
// deliberate change here too.
var siteAccountRoutes = map[string]string{
	"GET /api/v1/auth/me": "requireAuth",

	"GET /api/v1/account":                  "account",
	"GET /api/v1/account/usage":            "account",
	"PATCH /api/v1/account":                "account",
	"POST /api/v1/account/password":        "limitedAccount",
	"POST /api/v1/account/2fa":             "limitedAccount",
	"POST /api/v1/account/2fa/confirm":     "limitedAccount",
	"DELETE /api/v1/account/sessions":      "account",
	"DELETE /api/v1/account/sessions/{id}": "account",

	"GET /api/v1/sites":                           "readAccount",
	"GET /api/v1/sites/{id}":                      "readSite",
	"GET /api/v1/sites/{id}/disk":                 "readSite",
	"GET /api/v1/php/versions":                    "readAccount",
	"GET /api/v1/ssl":                             "readAccount",
	"GET /api/v1/databases":                       "readAccount",
	"GET /api/v1/cron":                            "readAccount",
	"GET /api/v1/logs":                            "readAccount",
	"GET /api/v1/logs/stream":                     "readAccount",
	"POST /api/v1/sites":                          "account",
	"DELETE /api/v1/sites/{id}":                   "siteAuthed",
	"POST /api/v1/sites/{id}/php":                 "siteAuthed",
	"PUT /api/v1/sites/{id}/php-settings":         "siteAuthed",
	"PUT /api/v1/sites/{id}/redirects":            "siteAuthed",
	"GET /api/v1/sites/{id}/sftp":                 "siteAuthed",
	"PUT /api/v1/sites/{id}/sftp":                 "siteAuthed",
	"POST /api/v1/sites/{id}/sftp/password":       "siteAuthed",
	"PUT /api/v1/sites/{id}/page-cache":           "siteAuthed",
	"POST /api/v1/sites/{id}/page-cache/purge":    "siteAuthed",
	"GET /api/v1/sites/{id}/object-cache":         "siteAuthed",
	"PUT /api/v1/sites/{id}/object-cache":         "siteAuthed",
	"POST /api/v1/sites/{id}/object-cache/flush":  "siteAuthed",
	"POST /api/v1/sites/{id}/object-cache/resume": "siteAuthed",
	"POST /api/v1/sites/{id}/ssl":                 "siteAuthed",
	"DELETE /api/v1/sites/{id}/ssl":               "siteAuthed",
	"POST /api/v1/sites/{id}/ssl/renew":           "siteAuthed",
	"POST /api/v1/sites/{id}/cron":                "siteAuthed",
	"GET /api/v1/sites/{id}/wp-admin":             "siteAuthed",
	"GET /api/v1/wp-admin.js":                     "account",
	"GET /api/v1/sites/{id}/wp-debug":             "siteAuthed",
	"PUT /api/v1/sites/{id}/wp-debug":             "siteAuthed",
	"GET /api/v1/sites/{id}/files":                "siteAuthed",
	"GET /api/v1/sites/{id}/files/content":        "siteAuthed",
	"PUT /api/v1/sites/{id}/files/content":        "siteAuthed",
	"GET /api/v1/sites/{id}/files/download":       "siteAuthed",
	"POST /api/v1/sites/{id}/files/upload":        "siteAuthed",
	"POST /api/v1/sites/{id}/files/{op}":          "siteAuthed",
	"PATCH /api/v1/cron/{id}":                     "account",
	"DELETE /api/v1/cron/{id}":                    "account",
	"POST /api/v1/databases/{name}/password":      "account",
	"POST /api/v1/databases/{name}/export":        "account",
	"POST /api/v1/databases/{name}/import":        "account",
	"GET /api/v1/jobs/{id}":                       "account",
	"GET /api/v1/jobs/{id}/events":                "account",
	"GET /api/v1/jobs/{id}/secret":                "account",
}

// adminOnly are the wrappers that keep site accounts out.
var adminOnly = []string{"authed", "read"}

// unauthenticated routes (login steps, health) aren't part of the matrix.
var publicWrappers = []string{"rateLimited", "requireAnySession", "http", "handleHealth"}

type route struct{ pattern, wrapper string }

// routes reads the route table from server.go, so a new route can't be
// added without this test seeing it.
func routes(t *testing.T) []route {
	t.Helper()
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	rx := regexp.MustCompile(`mux\.Handle(?:Func)?\("([A-Z]+ /api/[^"]+)", (?:s\.)?(\w+)`)
	var out []route
	for _, m := range rx.FindAllStringSubmatch(string(src), -1) {
		out = append(out, route{m[1], m[2]})
	}
	if len(out) < 70 {
		t.Fatalf("only %d routes parsed from server.go", len(out))
	}
	return out
}

func TestRouteAccessTable(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range routes(t) {
		want, open := siteAccountRoutes[r.pattern]
		switch {
		case open:
			seen[r.pattern] = true
			if r.wrapper != want {
				t.Errorf("%s uses %s, want %s", r.pattern, r.wrapper, want)
			}
		case slices.Contains(adminOnly, r.wrapper), slices.Contains(publicWrappers, r.wrapper):
		default:
			t.Errorf("%s uses %s: site accounts can reach it; add it to siteAccountRoutes or make it admin-only", r.pattern, r.wrapper)
		}
	}
	for p := range siteAccountRoutes {
		if !seen[p] {
			t.Errorf("siteAccountRoutes lists %s, which server.go doesn't register", p)
		}
	}
}

// TestSiteAccountForbidden calls every admin-only route as a site account
// (403), and every site route for a stranger's site (404).
func TestSiteAccountForbidden(t *testing.T) {
	e := newTestEnv(t)
	main, _, other := accountSites(t, e)
	c := e.siteUser(t, "client", main)
	path := func(p string, site int64) string {
		_, p, _ = strings.Cut(p, " ")
		p = strings.ReplaceAll(p, "{id}", fmt.Sprint(site))
		return regexp.MustCompile(`\{\w+\}`).ReplaceAllString(p, "x")
	}
	for _, r := range routes(t) {
		method, _, _ := strings.Cut(r.pattern, " ")
		switch {
		case slices.Contains(adminOnly, r.wrapper):
			st, body := c.do(method, path(r.pattern, main), map[string]any{})
			if st != 403 || errCode(body) != "forbidden" {
				t.Errorf("site account: %s = %d %v, want 403 forbidden", r.pattern, st, body)
			}
		case r.wrapper == "siteAuthed" || r.wrapper == "readSite":
			st, body := c.do(method, path(r.pattern, other), map[string]any{})
			if st != 404 || errCode(body) != "not_found" {
				t.Errorf("site account, stranger's site: %s = %d %v, want 404", r.pattern, st, body)
			}
		}
	}
}

func TestSiteAccountLists(t *testing.T) {
	e := newTestEnv(t)
	main, sub, other := accountSites(t, e)
	ctx := context.Background()
	mine, _ := e.st.AddCronJob(ctx, store.CronJob{SiteID: sub, Schedule: "* * * * *", Command: "true", Enabled: true})
	theirs, _ := e.st.AddCronJob(ctx, store.CronJob{SiteID: other, Schedule: "* * * * *", Command: "true", Enabled: true})
	c := e.siteUser(t, "client", main)

	st, body := c.do("GET", "/api/v1/sites", nil)
	if st != 200 {
		t.Fatalf("sites: %d %v", st, body)
	}
	var domains []string
	for _, x := range body["sites"].([]any) {
		domains = append(domains, x.(map[string]any)["domain"].(string))
	}
	if !slices.Equal(domains, []string{"blog.example.com", "example.com"}) {
		t.Errorf("site account sees %v", domains)
	}

	st, body = c.do("GET", "/api/v1/cron", nil)
	if st != 200 || len(body["jobs"].([]any)) != 1 {
		t.Errorf("cron: %d %v", st, body)
	}
	if st, _ := c.do("DELETE", fmt.Sprintf("/api/v1/cron/%d", theirs), nil); st != 404 {
		t.Errorf("delete a stranger's cron job: %d, want 404", st)
	}
	if st, _ := c.do("PATCH", fmt.Sprintf("/api/v1/cron/%d", theirs), map[string]bool{"enabled": false}); st != 404 {
		t.Errorf("change a stranger's cron job: %d, want 404", st)
	}
	_ = mine

	// The main site can't be deleted by its account.
	if st, body := c.do("DELETE", fmt.Sprintf("/api/v1/sites/%d", main), map[string]string{"confirm_domain": "example.com"}); st != 403 {
		t.Errorf("delete main site: %d %v, want 403", st, body)
	}

	// The admin still sees everything.
	a := e.loggedIn(t)
	if _, body := a.do("GET", "/api/v1/sites", nil); len(body["sites"].([]any)) != 3 {
		t.Errorf("admin sees %d sites", len(body["sites"].([]any)))
	}
	if st, body := a.do("GET", "/api/v1/auth/me", nil); st != 200 || body["user"].(map[string]any)["role"] != "admin" {
		t.Errorf("admin me: %d %v", st, body)
	}
	if _, body := c.do("GET", "/api/v1/auth/me", nil); body["user"].(map[string]any)["site_id"] != float64(main) {
		t.Errorf("site account me: %v", body)
	}
}

func TestSiteAccountJobs(t *testing.T) {
	e := newTestEnv(t)
	main, _, _ := accountSites(t, e)
	ctx := context.Background()
	admin, _ := e.st.UserByUsername(ctx, "admin")
	if err := e.st.CreateJob(ctx, store.JobRow{ID: "adminjob", Kind: "site.create", SiteID: main, UserID: admin.ID, CreatedAt: e.now}); err != nil {
		t.Fatal(err)
	}
	c := e.siteUser(t, "client", main)
	if st, _ := c.do("GET", "/api/v1/jobs/adminjob", nil); st != 404 {
		t.Errorf("site account reads an admin's job: %d, want 404", st)
	}
	if st, _ := c.do("GET", "/api/v1/jobs/adminjob/events", nil); st != 404 {
		t.Errorf("site account streams an admin's job: %d, want 404", st)
	}
	if st, _ := e.loggedIn(t).do("GET", "/api/v1/jobs/adminjob", nil); st != 200 {
		t.Errorf("admin reads the job: %d", st)
	}
}
