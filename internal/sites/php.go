package sites

import (
	"context"
	"errors"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// SwitchPHP starts a job that moves a site whose configs Layte renders to another PHP version
// without downtime:
//
//  1. start a pool on the new version (its own socket, so both run side by side)
//  2. point nginx at the new socket (nginx -t, reload)
//  3. remove the old pool
//
// If step 1 or 2 fails, the finished steps are undone and the site keeps
// running on the old version.
func (s *Service) SwitchPHP(ctx context.Context, actor agent.Actor, site store.Site, version string) (string, error) {
	if err := s.requireManagedActive(site); err != nil {
		return "", err
	}
	var installed []string
	if err := s.Agent.Call(ctx, "php.versions", actor, nil, &installed); err != nil {
		return "", err
	}
	if err := validate.PHPVersion(version, installed); err != nil {
		return "", badRequest("%v", err)
	}
	if version == site.PHPVersion {
		return "", badRequest("%s already uses PHP %s", site.Domain, version)
	}

	oldVersion := site.PHPVersion
	cfg := func(v string) actions.SiteConfigParams {
		c := s.siteConfig(site, site.SSL)
		c.PHPVersion = v
		return c
	}
	jobID, err := s.Jobs.Start("site.php_switch", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) (err error) {
		var undo undoStack
		defer func() {
			if err != nil {
				j.FailStep()
				j.Step("Rolling back")
				undo.run(j)
				j.Log("%s still runs on PHP %s.", site.Domain, oldVersion)
			}
		}()
		call := func(action string, params any) error { return s.Agent.Call(ctx, action, actor, params, nil) }

		if site.ObjectCache.Enabled {
			j.Step("Install Redis for PHP " + version + " (object cache)")
			if err := s.longCall(ctx, 10*time.Minute, "object_cache.prepare", actor, actions.ObjectCachePrepareParams{PHPVersion: version}, nil); err != nil {
				return err
			}
		}
		j.Step("Start PHP " + version + " pool")
		if err := call("php.pool_write", cfg(version)); err != nil {
			return err
		}
		undo.push("remove PHP "+version+" pool", func(ctx context.Context) error {
			return s.Agent.Call(ctx, "php.pool_remove", actor, poolRemoveParams(site, version), nil)
		})

		j.Step("Point nginx to PHP " + version)
		if err := call("nginx.vhost_write", cfg(version)); err != nil {
			return err
		}
		undo.push("point nginx back to PHP "+oldVersion, func(ctx context.Context) error {
			return s.Agent.Call(ctx, "nginx.vhost_write", actor, cfg(oldVersion), nil)
		})

		site.PHPVersion = version
		site.FPMPool = poolPath(site, version)
		if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
			return err
		}

		// From here the switch is done; cleaning up the old pool is best effort.
		j.Step("Remove PHP " + oldVersion + " pool")
		if err := call("php.pool_remove", poolRemoveParams(site, oldVersion)); err != nil {
			j.Log("Warning: couldn't remove the old pool (%v). It is unused and can be removed later.", err)
		}
		j.SetResult(map[string]any{"site_id": site.ID, "from": oldVersion, "to": version})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}
