package sites

import (
	"context"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
)

// QuotaStatus says whether disk quotas are on (and why they can't be).
func (s *Service) QuotaStatus(ctx context.Context, actor agent.Actor) (actions.QuotaStatus, error) {
	var st actions.QuotaStatus
	err := s.Agent.Call(ctx, "quota.status", actor, nil, &st)
	return st, err
}

// EnableQuotas turns on disk quotas for /home's filesystem (once, by an admin).
func (s *Service) EnableQuotas(ctx context.Context, actor agent.Actor) (actions.QuotaStatus, error) {
	var st actions.QuotaStatus
	err := s.longCall(ctx, 25*time.Minute, "quota.enable", actor, nil, &st)
	return st, err
}

// DisableQuotas turns disk quotas off. Nothing enforces a disk limit then,
// so every site login's disk limit goes back to 0 (none).
func (s *Service) DisableQuotas(ctx context.Context, actor agent.Actor) (actions.QuotaStatus, error) {
	var st actions.QuotaStatus
	if err := s.longCall(ctx, 2*time.Minute, "quota.disable", actor, nil, &st); err != nil {
		return st, err
	}
	users, err := s.Store.ListUsers(ctx)
	if err != nil {
		return st, err
	}
	for _, u := range users {
		if u.Limits.DiskMB != 0 {
			l := u.Limits
			l.DiskMB = 0
			if err := s.Store.SetLimits(ctx, u.ID, l); err != nil {
				return st, err
			}
		}
	}
	diskCache.Range(func(k, _ any) bool { diskCache.Delete(k); return true })
	return st, nil
}

// SetDiskQuota sets a Linux user's disk limit in MB (0 = none). Without
// quotas turned on, 0 is fine and anything else is refused.
func (s *Service) SetDiskQuota(ctx context.Context, actor agent.Actor, user string, mb int) error {
	if mb > 0 {
		st, err := s.QuotaStatus(ctx, actor)
		if err != nil {
			return err
		}
		if !st.Enabled {
			return conflict("quota_off", "turn on disk quotas first (Users → Disk quotas), or set the disk limit to 0")
		}
	}
	ForgetDiskUsage(user) // the next reading shows the new limit
	return s.Agent.Call(ctx, "quota.set", actor, actions.QuotaSetParams{Username: user, MB: mb}, nil)
}
