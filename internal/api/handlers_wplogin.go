package api

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/sites"
	"github.com/slightgrey/layte-panel/internal/store"
)

// One-click WP-Admin. The panel's WP-Admin button is a plain link (new tab)
// to GET /api/v1/sites/{id}/wp-admin. The session cookie is SameSite=Strict,
// so only the panel itself can open it. The page it returns POSTs a one-time
// token to that site's wp-login.php (so the token never appears in a URL, a
// log or the history), under a policy that allows form submission to that
// one site only. Layte's must-use plugin there turns the token into a normal
// WordPress login.

var wpAdminPage = template.Must(template.New("wp").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>Opening WP-Admin…</title>
<style>body{font:15px system-ui,sans-serif;display:grid;place-items:center;min-height:90vh;color:#555}</style></head>
<body>
{{if .Error}}<p>Couldn't open WP-Admin: {{.Error}}</p>
{{else}}<form id="layte-wp-login" method="post" action="{{.Action}}">
<input type="hidden" name="layte_login" value="{{.Token}}">
<p>Signing you in to WordPress… <noscript><button type="submit">Continue</button></noscript></p>
</form>
<script src="/api/v1/wp-admin.js"></script>{{end}}
</body></html>`))

const wpAdminScript = `document.getElementById("layte-wp-login").submit();` + "\n"

// GET /api/v1/wp-admin.js — submits the hand-off form (a file, so the page
// needs no inline script).
func (s *server) handleWPAdminScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(wpAdminScript))
}

// GET /api/v1/sites/{id}/wp-admin — signs the panel admin in to the site's
// WP-Admin (one-time token, valid 60 seconds).
func (s *server) handleWPAdmin(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")

	fail := func(status int, msg string) {
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		w.WriteHeader(status)
		wpAdminPage.Execute(w, map[string]string{"Error": msg})
	}
	site, ok := s.loadSiteQuiet(r)
	if !ok {
		fail(http.StatusNotFound, "no such site")
		return
	}
	login, err := s.sites.WPLogin(r.Context(), actorOf(r), site)
	if err != nil {
		var se *sites.Error
		var ae *agent.Error
		switch {
		case errors.As(err, &se):
			fail(se.Status, se.Message)
		case errors.As(err, &ae):
			s.Log.Warn("wp-admin login", "site", site.Domain, "code", ae.Code, "err", ae.Message)
			fail(http.StatusBadGateway, ae.Message)
		default:
			s.Log.Error("wp-admin login", "site", site.Domain, "err", err)
			fail(http.StatusBadGateway, "the server couldn't create a login")
		}
		return
	}
	u, err := url.Parse(login.ActionURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host != site.Domain {
		fail(http.StatusBadGateway, "unexpected WordPress address")
		return
	}
	// Form submission only to this site (both schemes: WordPress may move
	// between them on its own redirects); scripts only from the panel.
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; "+
		"form-action https://"+site.Domain+" http://"+site.Domain)
	wpAdminPage.Execute(w, map[string]string{"Action": login.ActionURL, "Token": login.Token})
}

// loadSiteQuiet is loadSite without writing a JSON error (the caller shows
// an HTML page instead).
func (s *server) loadSiteQuiet(r *http.Request) (store.Site, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return store.Site{}, false
	}
	site, err := s.Store.SiteByID(r.Context(), id)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.Log.Error("load site", "err", err)
		}
		return store.Site{}, false
	}
	return site, true
}
