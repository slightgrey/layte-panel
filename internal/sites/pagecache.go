package sites

import (
	"context"
	"errors"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/pagecache"
	"github.com/slightgrey/layte-panel/internal/store"
)

// requireCacheable: the page cache is for WordPress sites whose nginx config
// Layte renders (its skip rules know WordPress): created, or adopted and
// taken over.
func requireCacheable(site store.Site) error {
	switch {
	case !site.LayteConfigs():
		return conflict("site_unmanaged", "the page cache needs Layte Panel to manage the site's nginx config")
	case !site.WordPress:
		return badRequest("the page cache is only available on WordPress sites")
	case site.Status != "active":
		return conflict("site_busy", "%s is %s", site.Domain, site.Status)
	}
	return nil
}

// SetPageCache starts a job that turns the page cache on or off (or changes
// its expiry). On: install the must-use plugin that asks for purges, then
// add the cache to the vhost (nginx -t, reload). Off: the reverse, then
// delete the cached pages. Failed steps are undone.
func (s *Service) SetPageCache(ctx context.Context, actor agent.Actor, site store.Site, next pagecache.Settings) (string, error) {
	if err := requireCacheable(site); err != nil {
		return "", err
	}
	if err := next.Validate(); err != nil {
		return "", badRequest("%v", err)
	}
	if !next.Enabled {
		next = pagecache.Settings{} // keep nothing while it's off
	}
	old := site.PageCache
	if next == old {
		return "", badRequest("nothing changed")
	}
	cfg := func(c pagecache.Settings) actions.SiteConfigParams {
		p := s.siteConfig(site, site.SSL)
		p.Cache = c
		return p
	}
	cache := cacheParams(site)

	jobID, err := s.Jobs.Start("site.page_cache", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) (err error) {
		var undo undoStack
		defer func() {
			if err != nil {
				j.FailStep()
				j.Step("Rolling back")
				undo.run(j)
				j.Log("%s keeps its previous page cache setting.", site.Domain)
			}
		}()
		call := func(action string, params any) error { return s.Agent.Call(ctx, action, actor, params, nil) }

		if next.Enabled && !old.Enabled {
			j.Step("Install the purge plugin (clears the cache when content changes)")
			install := cache
			install.Install = true
			if err := call("cache.plugin", install); err != nil {
				return err
			}
			undo.push("remove the purge plugin", func(ctx context.Context) error {
				return s.Agent.Call(ctx, "cache.plugin", actor, cache, nil)
			})
		}

		j.Step("Update nginx")
		if err := call("nginx.vhost_write", cfg(next)); err != nil {
			return err
		}
		undo.push("restore the nginx config", func(ctx context.Context) error {
			return s.Agent.Call(ctx, "nginx.vhost_write", actor, cfg(old), nil)
		})

		if !next.Enabled {
			// From here the cache is off; cleaning up is best effort.
			j.Step("Remove the purge plugin and the cached pages")
			if err := call("cache.plugin", cache); err != nil {
				j.Log("Warning: couldn't remove the purge plugin (%v).", err)
			}
			remove := cache
			remove.Remove = true
			if err := call("cache.purge", remove); err != nil {
				j.Log("Warning: couldn't delete the cached pages (%v).", err)
			}
		}

		site.PageCache = next
		if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
			return err
		}
		j.SetResult(map[string]any{"site_id": site.ID, "page_cache": next})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}

// PurgeCache deletes the site's cached pages now.
func (s *Service) PurgeCache(ctx context.Context, actor agent.Actor, site store.Site) error {
	if err := requireCacheable(site); err != nil {
		return err
	}
	if !site.PageCache.Enabled {
		return badRequest("the page cache is off")
	}
	return s.longCall(ctx, time.Minute, "cache.purge", actor, cacheParams(site), nil)
}

// cacheParams: adopted sites have WordPress right in their web root.
func cacheParams(site store.Site) actions.CacheParams {
	p := actions.CacheParams{Domain: site.Domain, Username: site.SystemUser}
	if site.Adopted {
		p.WebRoot = site.RootPath
	}
	return p
}
