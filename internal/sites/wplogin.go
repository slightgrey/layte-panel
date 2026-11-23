package sites

import (
	"context"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/store"
)

// WPLogin gets a one-time WP-Admin login for a WordPress site created or
// adopted by Layte (token valid 60 seconds, single use).
func (s *Service) WPLogin(ctx context.Context, actor agent.Actor, site store.Site) (actions.WPLogin, error) {
	var out actions.WPLogin
	switch {
	case !site.WordPress:
		return out, badRequest("%s isn't a WordPress site", site.Domain)
	case !site.Managed && !site.Adopted:
		return out, conflict("site_unmanaged", "%s is imported read-only; adopt it to use one-click WP-Admin", site.Domain)
	case site.Status != "active":
		return out, conflict("site_busy", "%s is %s", site.Domain, site.Status)
	case site.PHPVersion == "" || site.SystemUser == "":
		return out, conflict("site_incomplete", "%s has no PHP version or site user", site.Domain)
	}
	params := actions.WPLoginParams{Domain: site.Domain, Username: site.SystemUser, WebRoot: adoptedRoot(site), PHPVersion: site.PHPVersion}
	err := s.longCall(ctx, time.Minute, "wp.login_token", actor, params, &out)
	return out, err
}
