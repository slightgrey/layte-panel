package sites

import (
	"context"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/store"
)

// Usage is a site login's use of its limits.
type Usage struct {
	actions.AccountUsage
	Limits        store.Limits `json:"limits"`
	Subdomains    int          `json:"subdomains"`
	MaxSubdomains int          `json:"max_subdomains"`
}

// AccountUsage reads what the site login u uses now.
func (s *Service) AccountUsage(ctx context.Context, actor agent.Actor, u store.User) (Usage, error) {
	main, err := s.Store.SiteByID(ctx, u.SiteID)
	if err != nil {
		return Usage{}, err
	}
	out := Usage{Limits: u.Limits, MaxSubdomains: u.MaxSubdomains}
	out.Subdomains, _ = s.Store.CountSubdomains(ctx, u.SiteID)
	err = s.Agent.Call(ctx, "account.usage", actor, actions.UserParams{Username: main.SystemUser}, &out.AccountUsage)
	return out, err
}
