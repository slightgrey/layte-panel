package api

import (
	"errors"
	"fmt"
	"github.com/slightgrey/layte-panel/internal/pagecache"
	"github.com/slightgrey/layte-panel/internal/phpini"
	"github.com/slightgrey/layte-panel/internal/redirects"
	"net"
	"net/http"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/sites"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// writeSiteError maps orchestrator errors to API errors.
func (s *server) writeSiteError(w http.ResponseWriter, what string, err error) {
	var se *sites.Error
	var ae *agent.Error
	switch {
	case errors.As(err, &se):
		writeError(w, se.Status, se.Code, se.Message)
	case errors.Is(err, jobs.ErrSiteBusy):
		writeError(w, http.StatusConflict, "site_busy", err.Error())
	case errors.As(err, &ae):
		e := s.agentError(what, err)
		writeError(w, http.StatusBadGateway, e.Code, e.Message)
	default:
		e := s.agentError(what, err) // socket down etc.
		if e.Code == "agent_unavailable" {
			writeError(w, http.StatusBadGateway, e.Code, e.Message)
			return
		}
		s.internalError(w, what, err)
	}
}

// POST /api/v1/sites — start creating a site. 202 {job_id, site_id}.
func (s *server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	var req sites.CreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if si, _ := sessionFrom(r.Context()); isSiteUser(r) {
		// Site accounts add subdomain sites of their main site, up to their limit.
		req.ParentSiteID, req.Aliases, req.Username = si.User.SiteID, nil, ""
		n, err := s.Store.CountSubdomains(r.Context(), si.User.SiteID)
		if err != nil {
			s.internalError(w, "count subdomains", err)
			return
		}
		if n >= si.User.MaxSubdomains {
			writeError(w, http.StatusConflict, "limit_reached", fmt.Sprintf("this account may have %d subdomain site(s); ask your admin for more", si.User.MaxSubdomains))
			return
		}
	}
	req.SaveSSLEmail = !isSiteUser(r)
	req.ReservedHosts = panelHosts(r)
	jobID, siteID, err := s.sites.Create(r.Context(), actorOf(r), req)
	if err != nil {
		s.writeSiteError(w, "create site", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": siteID})
}

// POST /api/v1/sites/{id}/php {version} — switch PHP version. 202 {job_id}.
func (s *server) handleSwitchPHP(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	jobID, err := s.sites.SwitchPHP(r.Context(), actorOf(r), site, req.Version)
	if err != nil {
		s.writeSiteError(w, "switch php", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}

// PUT /api/v1/sites/{id}/php-settings {memory_limit, upload_max, max_execution_time, max_input_vars} — 202 {job_id}.
func (s *server) handleSetPHPSettings(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req phpini.Settings
	if !decodeJSON(w, r, &req) {
		return
	}
	jobID, err := s.sites.SetPHPSettings(r.Context(), actorOf(r), site, req)
	if err != nil {
		s.writeSiteError(w, "php settings", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}

// PUT /api/v1/sites/{id}/redirects {redirects: [{from, to, code}]} — 202 {job_id}.
// The list replaces the site's redirects; an empty list removes them all.
func (s *server) handleSetRedirects(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req struct {
		Redirects []redirects.Redirect `json:"redirects"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	jobID, err := s.sites.SetRedirects(r.Context(), actorOf(r), site, req.Redirects)
	if err != nil {
		s.writeSiteError(w, "redirects", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}

// PUT /api/v1/sites/{id}/page-cache {enabled, ttl_minutes} — 202 {job_id}.
func (s *server) handleSetPageCache(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req pagecache.Settings
	if !decodeJSON(w, r, &req) {
		return
	}
	jobID, err := s.sites.SetPageCache(r.Context(), actorOf(r), site, req)
	if err != nil {
		s.writeSiteError(w, "page cache", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}

// POST /api/v1/sites/{id}/page-cache/purge — delete the cached pages now.
func (s *server) handlePurgeCache(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	if err := s.sites.PurgeCache(r.Context(), actorOf(r), site); err != nil {
		s.writeSiteError(w, "purge cache", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/sites/{id} {confirm_domain, remove_files, drop_db} — 202 {job_id}.
func (s *server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req sites.DeleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if isSiteUser(r) && site.ParentSiteID == 0 {
		writeError(w, http.StatusForbidden, "forbidden", "only an admin can delete the main site of an account")
		return
	}
	jobID, err := s.sites.Delete(r.Context(), actorOf(r), site, req)
	if err != nil {
		s.writeSiteError(w, "delete site", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}

// panelHosts are the names this panel is reached by (the Host of the
// request, normalised): no site may take them.
func panelHosts(r *http.Request) []string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if d, err := validate.Domain(host); err == nil {
		return []string{d}
	}
	return nil
}

// PUT /api/v1/sites/{id}/php-shell {block} — admins: block or allow PHP's
// shell functions (exec, system, …) on a site.
func (s *server) handleSetPHPShell(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req struct {
		Block bool `json:"block"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.sites.SetPHPShell(r.Context(), actorOf(r), site, req.Block); err != nil {
		s.writeSiteError(w, "php shell", err)
		return
	}
	s.auditPanel(r, "site.php_shell", map[string]any{"site": site.Domain, "block": req.Block})
	site, _ = s.Store.SiteByID(r.Context(), site.ID)
	writeJSON(w, http.StatusOK, site)
}
