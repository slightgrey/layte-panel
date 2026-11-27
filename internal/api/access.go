package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/slightgrey/layte-panel/internal/store"
)

// Who may do what
//
//   - Admins (role "admin") may use every endpoint.
//   - Site accounts (role "site") may only use the endpoints wrapped in
//     siteAuthed or account, and only for their own sites: the main site
//     (users.site_id) and its subdomains (sites.parent_site_id).
//
// Routes default to admin-only (authed). Opening one to site accounts is an
// explicit choice in server.go, and TestRouteAccess checks the full list.

// siteScope is the set of site ids a request may see; nil means all of them
// (admins and API keys, which only admins can create).
type siteScope map[int64]bool

func (sc siteScope) has(id int64) bool { return sc == nil || sc[id] }

// scopeOf returns the sites the request may see.
func (s *server) scopeOf(r *http.Request) (siteScope, error) {
	si, ok := sessionFrom(r.Context())
	if !ok || si.User.IsAdmin() {
		return nil, nil
	}
	return s.accountScope(r.Context(), si.User)
}

func (s *server) accountScope(ctx context.Context, u store.User) (siteScope, error) {
	sites, err := s.Store.SitesForAccount(ctx, u.SiteID)
	if err != nil {
		return nil, err
	}
	sc := siteScope{}
	for _, x := range sites {
		sc[x.ID] = true
	}
	return sc, nil
}

// isSiteUser reports whether the request comes from a site account.
func isSiteUser(r *http.Request) bool {
	si, ok := sessionFrom(r.Context())
	return ok && !si.User.IsAdmin()
}

// requireAdmin allows only fully logged-in admins.
func (s *server) requireAdmin(next http.Handler) http.Handler {
	return s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSiteUser(r) {
			writeError(w, http.StatusForbidden, "forbidden", "only admins can do this")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// requireSiteAccess allows admins, and site accounts for the {id} site when
// it is one of theirs. Other sites answer 404, so their ids don't leak.
func (s *server) requireSiteAccess(next http.Handler) http.Handler {
	return s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSiteUser(r) {
			id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if err != nil || id <= 0 {
				writeError(w, http.StatusNotFound, "not_found", "no such site")
				return
			}
			sc, err := s.scopeOf(r)
			if err != nil {
				s.internalError(w, "site access", err)
				return
			}
			if !sc.has(id) {
				writeError(w, http.StatusNotFound, "not_found", "no such site")
				return
			}
		}
		next.ServeHTTP(w, r)
	}))
}

// ownsDatabase reports whether a site account may manage the database: it
// must belong to one of the account's sites. Admins may manage any.
func (s *server) ownsDatabase(r *http.Request, name string) (bool, error) {
	if !isSiteUser(r) {
		return true, nil
	}
	sc, err := s.scopeOf(r)
	if err != nil {
		return false, err
	}
	db, err := s.Store.DatabaseByName(r.Context(), name)
	if err == nil && db.SiteID > 0 {
		return sc.has(db.SiteID), nil
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	// Sites created before the databases table: the name is on the site row.
	for id := range sc {
		if site, err := s.Store.SiteByID(r.Context(), id); err == nil && site.DBName == name {
			return true, nil
		}
	}
	return false, nil
}

// ownsCron reports whether the request may change the cron job.
func (s *server) ownsCron(r *http.Request, id int64) (bool, error) {
	if !isSiteUser(r) {
		return true, nil
	}
	job, err := s.Store.CronJobByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sc, err := s.scopeOf(r)
	if err != nil {
		return false, err
	}
	return sc.has(job.SiteID), nil
}

// ownsJob reports whether the request may read the job: site accounts only
// see the jobs they started themselves.
func (s *server) ownsJob(r *http.Request, row store.JobRow) bool {
	if !isSiteUser(r) {
		return true
	}
	si, _ := sessionFrom(r.Context())
	return row.UserID == si.User.ID
}
