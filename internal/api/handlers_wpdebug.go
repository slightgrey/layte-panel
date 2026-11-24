package api

import "net/http"

// GET /api/v1/sites/{id}/wp-debug — {enabled, log_path}.
func (s *server) handleGetWPDebug(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	state, err := s.sites.WPDebug(r.Context(), actorOf(r), site)
	if err != nil {
		s.writeSiteError(w, "wp debug", err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// PUT /api/v1/sites/{id}/wp-debug {enabled} — turn WP Debug on or off.
func (s *server) handleSetWPDebug(w http.ResponseWriter, r *http.Request) {
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
	state, err := s.sites.SetWPDebug(r.Context(), actorOf(r), site, req.Enabled)
	if err != nil {
		s.writeSiteError(w, "wp debug", err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}
