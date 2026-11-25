package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/store"
)

// actorOf is who an agent call is made for: the admin, or "api:<key name>".
func actorOf(r *http.Request) agent.Actor {
	if k, ok := keyFrom(r.Context()); ok {
		return agent.Actor{Username: "api:" + k.Name}
	}
	si, _ := sessionFrom(r.Context())
	return agent.Actor{UserID: si.User.ID, Username: si.User.Username}
}

// loadSite reads the {id} path value and the site; it writes the error response itself.
func (s *server) loadSite(w http.ResponseWriter, r *http.Request) (store.Site, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such site")
		return store.Site{}, false
	}
	site, err := s.Store.SiteByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such site")
		return store.Site{}, false
	}
	if err != nil {
		s.internalError(w, "load site", err)
		return store.Site{}, false
	}
	return site, true
}

// GET /api/v1/sites
func (s *server) handleListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := s.Store.ListSites(r.Context())
	if err != nil {
		s.internalError(w, "list sites", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": sites})
}

// GET /api/v1/sites/{id}
func (s *server) handleGetSite(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, site)
}

// POST /api/v1/sites/scan — discover sites in the nginx config and sync the
// imported (read-only) ones. Managed sites are never changed by a scan.
func (s *server) handleScanSites(w http.ResponseWriter, r *http.Request) {
	var found []actions.DiscoveredSite
	if err := s.Agent.Call(r.Context(), "sites.discover", actorOf(r), nil, &found); err != nil {
		e := s.agentError("sites.discover", err)
		writeError(w, http.StatusBadGateway, e.Code, e.Message)
		return
	}
	rows := make([]store.Site, 0, len(found))
	for _, f := range found {
		rows = append(rows, store.Site{
			Domain: f.Domain, Aliases: f.Aliases, RootPath: f.Root, PHPVersion: f.PHPVersion,
			WordPress: f.WordPress, SSL: f.SSL, NginxConf: f.ConfigFile, CertPath: f.CertPath, Source: "imported",
		})
	}
	added, removed, err := s.Store.ReplaceImported(r.Context(), rows)
	if err != nil {
		s.internalError(w, "save scanned sites", err)
		return
	}
	sites, err := s.Store.ListSites(r.Context())
	if err != nil {
		s.internalError(w, "list sites", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": len(found), "added": added, "removed": removed, "sites": sites})
}

// GET /api/v1/sites/{id}/vhost — the site's nginx config, read-only.
func (s *server) handleSiteVhost(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	if site.NginxConf == "" {
		writeError(w, http.StatusNotFound, "not_found", "this site has no nginx config yet")
		return
	}
	var out struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Managed bool   `json:"managed"`
	}
	if err := s.Agent.Call(r.Context(), "nginx.vhost_read", actorOf(r), actions.VhostReadParams{Path: site.NginxConf}, &out); err != nil {
		e := s.agentError("nginx.vhost_read", err)
		status := http.StatusBadGateway
		if e.Code == agent.CodeNotFound {
			status = http.StatusNotFound
		}
		writeError(w, status, e.Code, e.Message)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/v1/sites/{id}/disk — the site folder's size on disk (du).
func (s *server) handleSiteDisk(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	p := actions.SiteDiskParams{Domain: site.Domain}
	switch {
	case site.Managed && site.SystemUser != "":
		p.Username = site.SystemUser
	case site.Adopted:
		p.Username, p.WebRoot = site.SystemUser, site.RootPath
	case !site.Managed && site.RootPath != "":
		p.WebRoot = site.RootPath
	default:
		writeError(w, http.StatusNotFound, "not_found", "this site has no folder to measure")
		return
	}
	var out actions.DiskUsage
	if err := s.Agent.Call(r.Context(), "site.disk_usage", actorOf(r), p, &out); err != nil {
		e := s.agentError("site.disk_usage", err)
		status := http.StatusBadGateway
		if e.Code == agent.CodeNotFound {
			status = http.StatusNotFound
		}
		writeError(w, status, e.Code, e.Message)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/v1/sites/{id}/adopt — what adopting would change (read-only).
func (s *server) handleAdoptPreview(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	p, err := s.sites.AdoptPreview(r.Context(), actorOf(r), site)
	if err != nil {
		s.writeSiteError(w, "adopt preview", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// POST /api/v1/sites/{id}/adopt {username, pool_hash} — adopt (job).
func (s *server) handleAdopt(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req struct {
		Username string `json:"username"`
		PoolHash string `json:"pool_hash"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	jobID, err := s.sites.Adopt(r.Context(), actorOf(r), site, req.Username, req.PoolHash)
	if err != nil {
		s.writeSiteError(w, "adopt site", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}

// GET /api/v1/sites/{id}/takeover — what letting Layte manage an adopted
// site's nginx config would change (nothing is changed).
func (s *server) handleTakeoverPreview(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	p, err := s.sites.TakeoverPreview(r.Context(), actorOf(r), site)
	if err != nil {
		s.writeSiteError(w, "takeover preview", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// POST /api/v1/sites/{id}/takeover {vhost_hash, email} — take over (job).
func (s *server) handleTakeover(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req struct {
		VhostHash string `json:"vhost_hash"`
		Email     string `json:"email"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	jobID, err := s.sites.Takeover(r.Context(), actorOf(r), site, req.VhostHash, req.Email)
	if err != nil {
		s.writeSiteError(w, "take over nginx config", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}

// GET /api/v1/php/versions
func (s *server) handlePHPVersions(w http.ResponseWriter, r *http.Request) {
	var versions []string
	if err := s.Agent.Call(r.Context(), "php.versions", actorOf(r), nil, &versions); err != nil {
		e := s.agentError("php.versions", err)
		writeError(w, http.StatusBadGateway, e.Code, e.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}
