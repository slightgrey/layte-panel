package sites

import (
	"context"
	"errors"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/phpini"
	"github.com/slightgrey/layte-panel/internal/store"
)

// SetPHPSettings starts a job that writes a managed site's PHP settings into
// its PHP-FPM pool (php-fpm -t, reload) and, when the upload size or the
// execution time changes, its nginx vhost too (client_max_body_size,
// fastcgi_read_timeout). If a step fails, the finished steps are undone.
func (s *Service) SetPHPSettings(ctx context.Context, actor agent.Actor, site store.Site, in phpini.Settings) (string, error) {
	if err := s.requireManagedActive(site); err != nil {
		return "", err
	}
	next := in.Normalize()
	if err := next.Validate(); err != nil {
		return "", badRequest("%v", err)
	}
	old := site.PHPSettings
	if next == old {
		return "", badRequest("nothing changed")
	}
	vhostToo := next.NginxBodySize() != old.NginxBodySize() || next.FastCGIReadTimeout() != old.FastCGIReadTimeout()

	cfg := func(p phpini.Settings) actions.SiteConfigParams {
		c := s.siteConfig(site, site.SSL)
		c.PHP = p
		return c
	}
	jobID, err := s.Jobs.Start("site.php_settings", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) (err error) {
		var undo undoStack
		defer func() {
			if err != nil {
				j.FailStep()
				j.Step("Rolling back")
				undo.run(j)
				j.Log("%s keeps its previous PHP settings.", site.Domain)
			}
		}()
		call := func(action string, params any) error { return s.Agent.Call(ctx, action, actor, params, nil) }

		j.Step("Update the PHP-FPM pool")
		if err := call("php.pool_write", cfg(next)); err != nil {
			return err
		}
		undo.push("restore the PHP-FPM pool", func(ctx context.Context) error {
			return s.Agent.Call(ctx, "php.pool_write", actor, cfg(old), nil)
		})

		if vhostToo {
			j.Step("Update nginx (upload size, timeout)")
			if err := call("nginx.vhost_write", cfg(next)); err != nil {
				return err
			}
			undo.push("restore the nginx config", func(ctx context.Context) error {
				return s.Agent.Call(ctx, "nginx.vhost_write", actor, cfg(old), nil)
			})
		}

		site.PHPSettings = next
		if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
			return err
		}
		j.SetResult(map[string]any{"site_id": site.ID, "php_settings": next})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}
