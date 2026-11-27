package sites

import (
	"context"
	"fmt"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/templates"
)

// A site login's resource limits: its sites' PHP runs in the login's own
// PHP-FPM master inside a systemd slice with the limits (see
// internal/actions/account.go), and so do its cron jobs.

// poolPath is where the site's pool for a PHP version lives.
func poolPath(site store.Site, version string) string {
	if site.OwnFPM {
		return templates.AccountPoolPath(site.SystemUser, version, site.Domain)
	}
	return templates.PoolPath(version, site.Domain)
}

// poolRemoveParams removes the site's pool for a PHP version from wherever it runs.
func poolRemoveParams(site store.Site, version string) actions.PoolRemoveParams {
	return actions.PoolRemoveParams{Domain: site.Domain, PHPVersion: version, OwnFPM: site.OwnFPM, Username: site.SystemUser}
}

// canOwnFPM: the site's pool and vhost are both Layte's, so its PHP can move
// to another master (the vhost must follow to the new socket).
func canOwnFPM(site store.Site) bool {
	return site.LayteConfigs() && site.PHPVersion != "" && site.SystemUser != ""
}

// moveFPM moves the site's PHP pool into (own) or out of its login's PHP-FPM
// master without downtime: the new pool starts first, then nginx switches
// to its socket, then the old pool goes. site is updated on success.
func (s *Service) moveFPM(ctx context.Context, actor agent.Actor, site *store.Site, own bool) error {
	if site.OwnFPM == own {
		return nil
	}
	call := func(action string, params any) error { return s.Agent.Call(ctx, action, actor, params, nil) }
	from, to := *site, *site
	to.OwnFPM = own
	if err := call("php.pool_write", s.siteConfig(to, to.SSL)); err != nil {
		return fmt.Errorf("start the new PHP pool for %s: %w", site.Domain, err)
	}
	if err := call("nginx.vhost_write", s.siteConfig(to, to.SSL)); err != nil {
		s.Agent.Call(context.WithoutCancel(ctx), "php.pool_remove", actor, poolRemoveParams(to, to.PHPVersion), nil)
		return fmt.Errorf("point nginx to the new PHP pool for %s: %w", site.Domain, err)
	}
	if err := s.Store.SetOwnFPM(context.WithoutCancel(ctx), site.ID, own, poolPath(to, to.PHPVersion)); err != nil {
		return err
	}
	*site = to
	site.FPMPool = poolPath(to, to.PHPVersion)
	// Done: removing the old pool is clean-up.
	if err := call("php.pool_remove", poolRemoveParams(from, from.PHPVersion)); err != nil {
		s.Log().Warn("remove old PHP pool", "site", site.Domain, "err", err)
	}
	if err := s.syncCron(ctx, actor, *site); err != nil {
		s.Log().Warn("move cron jobs", "site", site.Domain, "err", err)
	}
	return nil
}

func accountParams(u store.User, user string) actions.AccountParams {
	return actions.AccountParams{Username: user, CPUPercent: u.Limits.CPUPercent, MemoryMB: u.Limits.MemoryMB}
}

// ApplyAccount puts a site login's limits in place and moves its sites'
// PHP (and cron jobs) inside them. Sites still being created only get the
// flag; their creation job puts their PHP in the right place.
func (s *Service) ApplyAccount(ctx context.Context, actor agent.Actor, u store.User) error {
	main, err := s.Store.SiteByID(ctx, u.SiteID)
	if err != nil {
		return err
	}
	if err := s.longCall(ctx, 30*time.Second, "account.apply", actor, accountParams(u, main.SystemUser), nil); err != nil {
		return err
	}
	if err := s.SetDiskQuota(ctx, actor, main.SystemUser, u.Limits.DiskMB); err != nil {
		return err
	}
	sites, err := s.Store.SitesForAccount(ctx, u.SiteID)
	if err != nil {
		return err
	}
	for _, x := range sites {
		if x.OwnFPM || !canOwnFPM(x) || x.SystemUser != main.SystemUser {
			continue
		}
		if x.Status == "creating" {
			if err := s.Store.SetOwnFPM(ctx, x.ID, true, poolPath(x, x.PHPVersion)); err != nil {
				return err
			}
			continue
		}
		if x.Status != "active" {
			continue // a failed or deleting site keeps its PHP where it is
		}
		if err := s.moveFPM(ctx, actor, &x, true); err != nil {
			return err
		}
	}
	return nil
}

// SetAccountLimits saves and applies new limits for a site login.
func (s *Service) SetAccountLimits(ctx context.Context, actor agent.Actor, u store.User, l store.Limits) error {
	if err := s.Store.SetLimits(ctx, u.ID, l); err != nil {
		return err
	}
	u.Limits = l
	return s.ApplyAccount(ctx, actor, u)
}

// ReleaseAccount undoes ApplyAccount before a site login is deleted: its
// sites' PHP goes back to the shared PHP-FPM services, without limits.
func (s *Service) ReleaseAccount(ctx context.Context, actor agent.Actor, u store.User) error {
	main, err := s.Store.SiteByID(ctx, u.SiteID)
	if err != nil {
		return err
	}
	sites, err := s.Store.SitesForAccount(ctx, u.SiteID)
	if err != nil {
		return err
	}
	for _, x := range sites {
		if !x.OwnFPM {
			continue
		}
		if err := s.moveFPM(ctx, actor, &x, false); err != nil {
			return err
		}
	}
	if err := s.SetDiskQuota(ctx, actor, main.SystemUser, 0); err != nil {
		s.Log().Warn("remove disk quota", "user", main.SystemUser, "err", err)
	}
	return s.longCall(ctx, time.Minute, "account.remove", actor, actions.UserParams{Username: main.SystemUser}, nil)
}
