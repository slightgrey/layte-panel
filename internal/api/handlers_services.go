package api

import (
	"net/http"
	"strings"

	"github.com/slightgrey/layte-panel/internal/actions"
)

// GET /api/v1/system/services — every service with what it uses and the
// action it offers (reload, or restart where no reload exists).
func (s *server) handleServicesDetail(w http.ResponseWriter, r *http.Request) {
	var out []actions.ServiceDetail
	if err := s.Agent.Call(r.Context(), "services.detail", actorOf(r), nil, &out); err != nil {
		s.writeSiteError(w, "services", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": out})
}

// POST /api/v1/system/services/{name}/{op} — reload or restart one service
// (the agent allows only the action its table gives that service, after
// the service's config test).
func (s *server) handleServiceControl(w http.ResponseWriter, r *http.Request) {
	var out map[string]string
	p := actions.ServiceControlParams{Name: r.PathValue("name"), Op: r.PathValue("op")}
	if err := p.Validate(); err != nil { // the same table the agent checks
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if err := s.Agent.Call(r.Context(), "service.control", actorOf(r), p, &out); err != nil {
		s.writeSiteError(w, "service "+p.Op, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/v1/system/reboot — whether a reboot is needed or scheduled.
func (s *server) handleRebootStatus(w http.ResponseWriter, r *http.Request) {
	var st actions.RebootStatus
	if err := s.Agent.Call(r.Context(), "reboot.status", actorOf(r), nil, &st); err != nil {
		s.writeSiteError(w, "reboot status", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// POST /api/v1/system/reboot {confirm} — reboot in a minute; confirm must be
// the server's hostname.
func (s *server) handleReboot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var st actions.RebootStatus
	if err := s.Agent.Call(r.Context(), "reboot.status", actorOf(r), nil, &st); err != nil {
		s.writeSiteError(w, "reboot status", err)
		return
	}
	if st.Hostname == "" || !strings.EqualFold(strings.TrimSpace(req.Confirm), st.Hostname) {
		writeError(w, http.StatusBadRequest, "invalid", "type the server's name ("+st.Hostname+") to confirm")
		return
	}
	if err := s.Agent.Call(r.Context(), "system.reboot", actorOf(r), nil, &st); err != nil {
		s.writeSiteError(w, "reboot", err)
		return
	}
	s.Log.Warn("reboot scheduled from the panel", "by", actorOf(r).Username)
	writeJSON(w, http.StatusAccepted, st)
}

// DELETE /api/v1/system/reboot — cancel a scheduled reboot.
func (s *server) handleRebootCancel(w http.ResponseWriter, r *http.Request) {
	var st actions.RebootStatus
	if err := s.Agent.Call(r.Context(), "system.reboot_cancel", actorOf(r), nil, &st); err != nil {
		s.writeSiteError(w, "cancel reboot", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
