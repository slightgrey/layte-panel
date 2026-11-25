package api

import "net/http"

// GET /api/v1/sites/{id}/object-cache — on/off, and Redis's live numbers.
func (s *server) handleObjectCache(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	st, err := s.sites.ObjectCache(r.Context(), actorOf(r), site)
	if err != nil {
		s.writeSiteError(w, "object cache", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// PUT /api/v1/sites/{id}/object-cache {enabled} — 202 {job_id}.
func (s *server) handleSetObjectCache(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	jobID, err := s.sites.SetObjectCache(r.Context(), actorOf(r), site, req.Enabled)
	if err != nil {
		s.writeSiteError(w, "object cache", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}

// POST /api/v1/sites/{id}/object-cache/flush — empty the site's Redis.
func (s *server) handleFlushObjectCache(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	if err := s.sites.FlushObjectCache(r.Context(), actorOf(r), site); err != nil {
		s.writeSiteError(w, "object cache flush", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/sites/{id}/object-cache/resume — on again after it was
// disabled in WordPress.
func (s *server) handleResumeObjectCache(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	if err := s.sites.ResumeObjectCache(r.Context(), actorOf(r), site); err != nil {
		s.writeSiteError(w, "object cache resume", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
