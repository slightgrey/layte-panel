package sites

import (
	"context"
	"errors"
	"slices"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/redirects"
	"github.com/slightgrey/layte-panel/internal/store"
)

// SetRedirects starts a job that replaces the site's redirects and rewrites
// its nginx vhost (nginx -t, reload; a failed test keeps the old config).
func (s *Service) SetRedirects(ctx context.Context, actor agent.Actor, site store.Site, in []redirects.Redirect) (string, error) {
	if err := s.requireManagedActive(site); err != nil {
		return "", err
	}
	next, err := redirects.Clean(in)
	if err != nil {
		return "", badRequest("%v", err)
	}
	if slices.Equal(next, site.Redirects) || (len(next) == 0 && len(site.Redirects) == 0) {
		return "", badRequest("nothing changed")
	}
	jobID, err := s.Jobs.Start("site.redirects", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		j.Step("Update nginx")
		updated := site
		updated.Redirects = next
		if err := s.Agent.Call(ctx, "nginx.vhost_write", actor, s.siteConfig(updated, site.SSL), nil); err != nil {
			return err // WriteConfig kept the previous vhost
		}
		if updated.Redirects == nil {
			updated.Redirects = []redirects.Redirect{}
		}
		if err := s.Store.UpdateSite(context.WithoutCancel(ctx), updated); err != nil {
			return err
		}
		j.Log("%d redirect(s) active.", len(next))
		j.SetResult(map[string]any{"site_id": site.ID, "redirects": len(next)})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}
