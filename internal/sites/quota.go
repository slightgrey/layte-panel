package sites

import (
	"context"
	"sync"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
)

// Turning disk quotas on can take minutes (packages, the quota file), so it
// runs in the background on the server: the page can be left and come back,
// every page sees it's still busy, and a second click is refused.

// QuotaState is the quota status plus what Layte is doing about it.
type QuotaState struct {
	actions.QuotaStatus
	// Busy is "enabling" or "disabling" while that runs ("" otherwise).
	Busy string `json:"busy,omitempty"`
	// LastError is why the last turn on/off failed (cleared by the next one).
	LastError string `json:"last_error,omitempty"`
}

var quotaOp struct {
	sync.Mutex
	busy, lastErr string
}

// QuotaStatus says whether disk quotas are on (and why they can't be).
func (s *Service) QuotaStatus(ctx context.Context, actor agent.Actor) (actions.QuotaStatus, error) {
	var st actions.QuotaStatus
	err := s.Agent.Call(ctx, "quota.status", actor, nil, &st)
	return st, err
}

// QuotaState is QuotaStatus with the running or failed turn on/off.
func (s *Service) QuotaState(ctx context.Context, actor agent.Actor) (QuotaState, error) {
	quotaOp.Lock()
	out := QuotaState{Busy: quotaOp.busy, LastError: quotaOp.lastErr}
	quotaOp.Unlock()
	if out.Busy != "" {
		return out, nil // don't ask the agent mid-change; the page polls
	}
	st, err := s.QuotaStatus(ctx, actor)
	out.QuotaStatus = st
	return out, err
}

// startQuotaOp runs fn in the background as op ("enabling" or "disabling"),
// unless another one is running.
func (s *Service) startQuotaOp(op string, fn func(ctx context.Context) error) (QuotaState, error) {
	quotaOp.Lock()
	defer quotaOp.Unlock()
	if quotaOp.busy != "" {
		return QuotaState{Busy: quotaOp.busy}, conflict("quota_busy", "disk quotas are already being turned %s", map[string]string{"enabling": "on", "disabling": "off"}[quotaOp.busy])
	}
	quotaOp.busy, quotaOp.lastErr = op, ""
	go func() {
		// Not the request's context: it ends when the page is left.
		err := fn(context.Background())
		quotaOp.Lock()
		quotaOp.busy = ""
		if err != nil {
			quotaOp.lastErr = err.Error()
			s.Log().Warn("disk quotas", "op", op, "err", err)
		}
		quotaOp.Unlock()
	}()
	return QuotaState{Busy: op}, nil
}

// EnableQuotas starts turning on disk quotas for /home's filesystem (once,
// by an admin).
func (s *Service) EnableQuotas(actor agent.Actor) (QuotaState, error) {
	return s.startQuotaOp("enabling", func(ctx context.Context) error {
		return s.longCall(ctx, 25*time.Minute, "quota.enable", actor, nil, nil)
	})
}

// DisableQuotas starts turning disk quotas off. Nothing enforces a disk
// limit then, so every site login's disk limit goes back to 0 (none).
func (s *Service) DisableQuotas(actor agent.Actor) (QuotaState, error) {
	return s.startQuotaOp("disabling", func(ctx context.Context) error {
		if err := s.longCall(ctx, 2*time.Minute, "quota.disable", actor, nil, nil); err != nil {
			return err
		}
		users, err := s.Store.ListUsers(ctx)
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.Limits.DiskMB != 0 {
				l := u.Limits
				l.DiskMB = 0
				if err := s.Store.SetLimits(ctx, u.ID, l); err != nil {
					return err
				}
			}
		}
		diskCache.Range(func(k, _ any) bool { diskCache.Delete(k); return true })
		return nil
	})
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
