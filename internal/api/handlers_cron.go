package api

import (
	"net/http"
	"strconv"
)

// GET /api/v1/cron
func (s *server) handleListCron(w http.ResponseWriter, r *http.Request) {
	jobs, suggest, err := s.sites.ListCron(r.Context())
	if err != nil {
		s.internalError(w, "list cron", err)
		return
	}
	sc, err := s.scopeOf(r)
	if err != nil {
		s.internalError(w, "list cron", err)
		return
	}
	if sc != nil { // site accounts: their sites' jobs only
		visible := jobs[:0]
		for _, j := range jobs {
			if sc.has(j.SiteID) {
				visible = append(visible, j)
			}
		}
		jobs = visible
		for id := range suggest {
			if !sc.has(id) {
				delete(suggest, id)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "wp_cron": suggest})
}

// POST /api/v1/sites/{id}/cron {schedule, command}
func (s *server) handleAddCron(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req struct {
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id, err := s.sites.AddCron(r.Context(), actorOf(r), site, req.Schedule, req.Command)
	if err != nil {
		s.writeSiteError(w, "add cron", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func cronID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// checkCron writes a 404 unless the request may change the cron job.
func (s *server) checkCron(w http.ResponseWriter, r *http.Request) bool {
	ok, err := s.ownsCron(r, cronID(r))
	if err != nil {
		s.internalError(w, "cron access", err)
		return false
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such cron job")
	}
	return ok
}

// PATCH /api/v1/cron/{id} {enabled}
func (s *server) handleUpdateCron(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) || !s.checkCron(w, r) {
		return
	}
	if err := s.sites.SetCronEnabled(r.Context(), actorOf(r), cronID(r), req.Enabled); err != nil {
		s.writeSiteError(w, "update cron", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/cron/{id}
func (s *server) handleDeleteCron(w http.ResponseWriter, r *http.Request) {
	if !s.checkCron(w, r) {
		return
	}
	if err := s.sites.DeleteCron(r.Context(), actorOf(r), cronID(r)); err != nil {
		s.writeSiteError(w, "delete cron", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
