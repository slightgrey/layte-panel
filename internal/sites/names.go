package sites

import (
	"context"
	"regexp"
	"strings"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
)

// A new site must not take a name that something else already answers for:
// nginx prefers an exact server_name over a wildcard or another config's
// name, so a clash would quietly move traffic (the panel's own address
// included) to the new site, which could then get a certificate for it.
// Layte's database only knows the sites it created or scanned, so the live
// nginx configs are checked too.

// matchesServerName reports whether nginx would send name to a server
// block with this server_name pattern (exact, *.x, .x, x.*, or ~regex).
func matchesServerName(pattern, name string) bool {
	switch {
	case strings.HasPrefix(pattern, "~"):
		re, err := regexp.Compile(pattern[1:])
		return err == nil && re.MatchString(name) // a regex Go can't read: nginx's own check stays
	case strings.HasPrefix(pattern, "*."):
		return strings.HasSuffix(name, pattern[1:])
	case strings.HasPrefix(pattern, "."):
		return name == pattern[1:] || strings.HasSuffix(name, pattern)
	case strings.HasSuffix(pattern, ".*"):
		return strings.HasPrefix(name, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == name
}

// checkNginxNames refuses domains that a live nginx config already serves,
// and the panel's own hostnames (reserved).
func (s *Service) checkNginxNames(ctx context.Context, actor agent.Actor, domains, reserved []string) error {
	for _, d := range domains {
		for _, r := range reserved {
			if d == r {
				return conflict("domain_taken", "%s is this panel's own address", d)
			}
		}
	}
	var names actions.ServerNames
	if err := s.Agent.Call(ctx, "nginx.server_names", actor, nil, &names); err != nil {
		return err // fail closed: no site without the check
	}
	for _, d := range domains {
		for _, n := range names.Exact {
			if n == d {
				return conflict("domain_taken", "%s is already served by another nginx config on this server", d)
			}
		}
		for _, p := range names.Patterns {
			if matchesServerName(p, d) {
				return conflict("domain_taken", "%s is already served by another nginx config on this server (server_name %s)", d, p)
			}
		}
	}
	return nil
}
