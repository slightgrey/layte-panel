package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
)

// packagesParam reads ?packages=a,b (kept-back packages to install).
func packagesParam(r *http.Request) []string {
	var out []string
	for _, p := range strings.Split(r.URL.Query().Get("packages"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// writeAptError shows apt's own explanation (it holds no secrets and only
// admins see it): 409 for a broken state or a refused plan, else 502.
func (s *server) writeAptError(w http.ResponseWriter, what string, err error) {
	var ae *agent.Error
	if !errors.As(err, &ae) {
		s.writeSiteError(w, what, err)
		return
	}
	status := http.StatusBadGateway
	if ae.Code == "apt_broken" || ae.Code == agent.CodeInvalidParams {
		status = http.StatusConflict
	}
	writeError(w, status, ae.Code, ae.Message)
}

// GET /api/v1/updates/plan?packages=&fix=1 — what installing updates (or,
// with fix=1, repairing an interrupted install) would do. Nothing is changed.
func (s *server) handleUpdatesPlan(w http.ResponseWriter, r *http.Request) {
	p := actions.UpgradeParams{Packages: packagesParam(r), Fix: r.URL.Query().Get("fix") == "1"}
	if err := p.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute+30*time.Second)
	defer cancel()
	var plan actions.UpgradePlan
	if err := s.Agent.Call(ctx, "updates.plan", actorOf(r), p, &plan); err != nil {
		s.writeAptError(w, "plan updates", err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// POST /api/v1/updates/refresh — apt-get update (fresh package lists).
func (s *server) handleUpdatesRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute+30*time.Second)
	defer cancel()
	if err := s.Agent.Call(ctx, "updates.refresh", actorOf(r), nil, nil); err != nil {
		s.writeAptError(w, "refresh package lists", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/updates/apply {packages?, plan_hash} — install the previewed
// updates (job). packages = install those kept-back packages instead.
func (s *server) handleUpdatesApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Packages []string `json:"packages"`
		Fix      bool     `json:"fix"`
		PlanHash string   `json:"plan_hash"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	params := actions.UpgradeApplyParams{Packages: req.Packages, Fix: req.Fix, PlanHash: req.PlanHash}
	if err := params.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	actor := actorOf(r)
	what := "all pending updates"
	switch {
	case req.Fix:
		what = "the interrupted install (repair)"
	case len(req.Packages) > 0:
		what = strings.Join(req.Packages, ", ")
	}
	jobID, err := s.Jobs.Start("server.upgrade", 0, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		j.Step("Install " + what)
		j.Log("apt-get upgrade (never removes packages; your config files are kept). Services restart as needed; this can take several minutes.")
		ctx, cancel := context.WithTimeout(ctx, 45*time.Minute+30*time.Second)
		defer cancel()
		var res actions.UpgradeResult
		if err := s.Agent.Call(ctx, "updates.apply", actor, params, &res); err != nil {
			return err
		}
		for _, line := range strings.Split(res.Output, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				j.Log("%s", line)
			}
		}
		j.Step("Check sites")
		for _, c := range res.Sites {
			if c.After == c.Before {
				j.Log("%s: %d, as before", c.Domain, c.After)
			} else {
				j.Log("WARNING %s answered %d before and %d now: check it", c.Domain, c.Before, c.After)
			}
		}
		if len(res.RestartPending) > 0 {
			j.Log("Still running on the old libraries: %s. Ubuntu doesn't restart these automatically; the next reboot takes care of them.", strings.Join(res.RestartPending, ", "))
		}
		if res.RebootRequired {
			j.Log("A reboot is needed to finish (%s). Layte Panel doesn't reboot by itself; do it when it suits you.", strings.Join(res.RebootPackages, ", "))
		}
		j.SetResult(map[string]any{
			"installed": res.Installed, "sites_changed": res.SitesChanged,
			"reboot_required": res.RebootRequired, "restart_pending": res.RestartPending,
		})
		return nil
	})
	if err != nil {
		s.writeSiteError(w, "install updates", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}
