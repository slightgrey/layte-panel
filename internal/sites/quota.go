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
	return s.Agent.Call(ctx, "quota.set", actor, actions.QuotaSetParams{Username: user, MB: mb}, nil)
}
