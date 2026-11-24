package api

import (
	"errors"
	"github.com/slightgrey/layte-panel/internal/phpini"
	"net/http"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/sites"
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
	jobID, err := s.sites.Delete(r.Context(), actorOf(r), site, req)
	if err != nil {
		s.writeSiteError(w, "delete site", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}
