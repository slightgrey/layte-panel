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
	params, err := wpParams(site, "one-click WP-Admin")
	if err != nil {
		return out, err
	}
	err = s.longCall(ctx, time.Minute, "wp.login_token", actor, params, &out)
	return out, err
}

// wpParams checks that the site is a WordPress site Layte runs (created or
// adopted) and returns what the agent's wp.* actions need.
func wpParams(site store.Site, feature string) (actions.WPLoginParams, error) {
	switch {
	case !site.WordPress:
		return actions.WPLoginParams{}, badRequest("%s isn't a WordPress site", site.Domain)
	case !site.Managed && !site.Adopted:
		return actions.WPLoginParams{}, conflict("site_unmanaged", "%s is imported read-only; adopt it to use %s", site.Domain, feature)
	case site.Status != "active":
		return actions.WPLoginParams{}, conflict("site_busy", "%s is %s", site.Domain, site.Status)
	case site.PHPVersion == "" || site.SystemUser == "":
		return actions.WPLoginParams{}, conflict("site_incomplete", "%s has no PHP version or site user", site.Domain)
	}
	return actions.WPLoginParams{Domain: site.Domain, Username: site.SystemUser, WebRoot: adoptedRoot(site), PHPVersion: site.PHPVersion}, nil
}

// WPDebug reads whether WP_DEBUG is on, and where its messages go.
func (s *Service) WPDebug(ctx context.Context, actor agent.Actor, site store.Site) (actions.WPDebugState, error) {
	var out actions.WPDebugState
	params, err := wpParams(site, "WP Debug")
	if err != nil {
		return out, err
	}
	err = s.longCall(ctx, 30*time.Second, "wp.debug_get", actor, params, &out)
	return out, err
}

// SetWPDebug turns WP Debug on or off (wp-config.php; put back if the site
// stops answering like before).
func (s *Service) SetWPDebug(ctx context.Context, actor agent.Actor, site store.Site, enabled bool) (actions.WPDebugState, error) {
	var out actions.WPDebugState
	params, err := wpParams(site, "WP Debug")
	if err != nil {
		return out, err
	}
	err = s.longCall(ctx, 2*time.Minute, "wp.debug_set", actor, actions.WPDebugSetParams{WPLoginParams: params, Enabled: enabled}, &out)
	return out, err
}
