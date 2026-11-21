package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
)

// GET /api/v1/security — firewall, failed SSH logins, pending updates.
func (s *server) handleSecurity(w http.ResponseWriter, r *http.Request) {
	var ov actions.SecurityOverview
	if err := s.Agent.Call(r.Context(), "security.overview", actorOf(r), nil, &ov); err != nil {
		s.writeSiteError(w, "security overview", err)
		return
	}
	var up actions.Updates
	updErr := s.Agent.Call(r.Context(), "updates.pending", actorOf(r), nil, &up)
	out := map[string]any{"overview": ov, "updates": up}
	if updErr != nil {
		out["updates_error"] = s.agentError("updates.pending", updErr)
	}
	writeJSON(w, http.StatusOK, out)
}

// securityCall runs a mutating security action and answers 204 or an error.
func (s *server) securityCall(w http.ResponseWriter, r *http.Request, action string, params any) {
	if err := s.Agent.Call(r.Context(), action, actorOf(r), params, nil); err != nil {
		var status int
		e := s.agentError(action, err)
		switch e.Code {
		case agent.CodeInvalidParams:
			status, e.Message = http.StatusBadRequest, agentMessage(err)
		case "agent_unavailable":
			status = http.StatusBadGateway
		default:
			status, e.Message = http.StatusBadGateway, agentMessage(err)
		}
		writeError(w, status, e.Code, e.Message)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/security/ufw/rules {port, proto, from}
func (s *server) handleUFWAllow(w http.ResponseWriter, r *http.Request) {
	var p actions.UFWAllowParams
	if !decodeJSON(w, r, &p) {
		return
	}
	p.Port, p.From = strings.TrimSpace(p.Port), strings.TrimSpace(p.From)
	if err := p.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	s.securityCall(w, r, "ufw.allow", p)
}

// DELETE /api/v1/security/ufw/rules/{num} {expect}
func (s *server) handleUFWDelete(w http.ResponseWriter, r *http.Request) {
	var p actions.UFWDeleteParams
	if !decodeJSON(w, r, &p) {
		return
	}
	p.Num, _ = strconv.Atoi(r.PathValue("num"))
	s.securityCall(w, r, "ufw.delete", p)
}

// POST /api/v1/security/ufw/enabled {enable}
func (s *server) handleUFWEnable(w http.ResponseWriter, r *http.Request) {
	var p actions.UFWEnableParams
	if !decodeJSON(w, r, &p) {
		return
	}
	s.securityCall(w, r, "ufw.set_enabled", p)
}

// POST /api/v1/security/cloudflare {enabled} — "Behind Cloudflare" (real
// visitor IPs). enabled=true also rewrites the file with this version's list.
func (s *server) handleCloudflare(w http.ResponseWriter, r *http.Request) {
	var p actions.CloudflareParams
	if !decodeJSON(w, r, &p) {
		return
	}
	s.securityCall(w, r, "cloudflare.set_realip", p)
}

// POST /api/v1/security/updates — install security updates (job).
func (s *server) handleInstallUpdates(w http.ResponseWriter, r *http.Request) {
	actor := actorOf(r)
	jobID, err := s.Jobs.Start("server.updates", 0, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		j.Step("Install security updates")
		j.Log("Running unattended-upgrade. This can take several minutes; the panel stays usable.")
		ctx, cancel := context.WithTimeout(ctx, 35*time.Minute)
		defer cancel()
		var out struct {
			Output string `json:"output"`
		}
		if err := s.Agent.Call(ctx, "updates.install", actor, nil, &out); err != nil {
			return err
		}
		for _, line := range strings.Split(out.Output, " | ") {
			if line = strings.TrimSpace(line); line != "" {
				j.Log("%s", line)
			}
		}
		return nil
	})
	if err != nil {
		s.writeSiteError(w, "install updates", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}
