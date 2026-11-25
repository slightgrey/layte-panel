package sites

import (
	"context"
	"errors"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/jobs"
	"github.com/slightgrey/layte-panel/internal/store"
)

// ObjectCacheState is what the site page shows.
type ObjectCacheState struct {
	store.ObjectCache
	actions.ObjectCacheStatus
}

// ObjectCache returns the site's object cache state (live numbers while on).
func (s *Service) ObjectCache(ctx context.Context, actor agent.Actor, site store.Site) (ObjectCacheState, error) {
	st := ObjectCacheState{ObjectCache: site.ObjectCache}
	if !site.ObjectCache.Enabled {
		return st, nil
	}
	p, err := wpParams(site, "the object cache")
	if err != nil {
		return st, nil
	}
	err = s.Agent.Call(ctx, "object_cache.status", actor, actions.ObjectCacheParams{WPLoginParams: p}, &st.ObjectCacheStatus)
	return st, err
}

// SetObjectCache starts a job that turns the site's Redis object cache on
// (installing Redis if needed) or off. Turning it on while it's on updates
// or repairs it (and moves sites off the Redis Object Cache plugin that
// earlier versions used).
func (s *Service) SetObjectCache(ctx context.Context, actor agent.Actor, site store.Site, enabled bool) (string, error) {
	p, err := wpParams(site, "the object cache")
	if err != nil {
		return "", err
	}
	if !enabled && !site.ObjectCache.Enabled {
		return "", badRequest("the object cache is already off")
	}
	params := actions.ObjectCacheParams{WPLoginParams: p, RemovePlugin: site.ObjectCache.InstalledPlugin}
	jobID, err := s.Jobs.Start("site.object_cache", site.ID, actor.UserID, func(ctx context.Context, j *jobs.Job) error {
		if !enabled {
			j.Step("Turn off the object cache and stop Redis")
			if err := s.longCall(ctx, 2*time.Minute, "object_cache.disable", actor, params, nil); err != nil {
				return err
			}
			site.ObjectCache = store.ObjectCache{}
			return s.Store.UpdateSite(context.WithoutCancel(ctx), site)
		}
		j.Step("Turn on the object cache (Redis)")
		j.Log("Installs Redis if needed (a minute or two the first time). Every step is undone if the site answers differently afterwards.")
		var res actions.ObjectCacheResult
		if err := s.longCall(ctx, 10*time.Minute, "object_cache.enable", actor, params, &res); err != nil {
			return err
		}
		for _, step := range res.Steps {
			j.Log("%s", step)
		}
		site.ObjectCache = store.ObjectCache{Enabled: true} // Layte's own cache: no plugin to remember
		if err := s.Store.UpdateSite(context.WithoutCancel(ctx), site); err != nil {
			return err
		}
		j.SetResult(map[string]any{"site_id": site.ID, "keys": res.Keys})
		return nil
	})
	if errors.Is(err, jobs.ErrSiteBusy) {
		return "", conflict("site_busy", "another job is running for %s", site.Domain)
	}
	return jobID, err
}

// FlushObjectCache empties the site's Redis now.
func (s *Service) FlushObjectCache(ctx context.Context, actor agent.Actor, site store.Site) error {
	p, err := wpParams(site, "the object cache")
	if err != nil {
		return err
	}
	if !site.ObjectCache.Enabled {
		return badRequest("the object cache is off")
	}
	return s.longCall(ctx, 30*time.Second, "object_cache.flush", actor, actions.ObjectCacheParams{WPLoginParams: p}, nil)
}

// ResumeObjectCache turns the cache back on after it was disabled in
// WordPress (Settings → Object Cache). Redis is emptied first.
func (s *Service) ResumeObjectCache(ctx context.Context, actor agent.Actor, site store.Site) error {
	p, err := wpParams(site, "the object cache")
	if err != nil {
		return err
	}
	if !site.ObjectCache.Enabled {
		return badRequest("the object cache is off")
	}
	return s.longCall(ctx, 30*time.Second, "object_cache.resume", actor, actions.ObjectCacheParams{WPLoginParams: p}, nil)
}
