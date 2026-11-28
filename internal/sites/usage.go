package sites

import (
	"context"
	"sync"
	"time"

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
	// DiskCheckedAt is when disk use was last measured (it's cached).
	DiskCheckedAt time.Time `json:"disk_checked_at"`
}

// DiskCacheTTL is how long a measured disk use is reused. RAM and CPU are
// read fresh every time (cheap); disk use costs a quota read or a du over
// the user's files, so the sidebar's refreshes reuse it.
const DiskCacheTTL = 5 * time.Minute

type diskReading struct {
	bytes, limit int64
	exact        bool
	at           time.Time
}

// diskCache holds the last disk reading per Linux user.
var diskCache sync.Map // user → diskReading

// ForgetDiskUsage drops a user's cached disk use (its limit changed).
func ForgetDiskUsage(user string) { diskCache.Delete(user) }

// AccountUsage reads what the site login u uses now. fresh measures disk
// use too, instead of reusing a reading up to DiskCacheTTL old.
func (s *Service) AccountUsage(ctx context.Context, actor agent.Actor, u store.User, fresh bool) (Usage, error) {
	main, err := s.Store.SiteByID(ctx, u.SiteID)
	if err != nil {
		return Usage{}, err
	}
	out := Usage{Limits: u.Limits, MaxSubdomains: u.MaxSubdomains}
	out.Subdomains, _ = s.Store.CountSubdomains(ctx, u.SiteID)
	user := main.SystemUser
	cached, ok := diskCache.Load(user)
	measure := fresh || !ok || time.Since(cached.(diskReading).at) > DiskCacheTTL
	p := actions.AccountUsageParams{Username: user, Disk: measure}
	if err := s.Agent.Call(ctx, "account.usage", actor, p, &out.AccountUsage); err != nil {
		return out, err
	}
	if measure {
		r := diskReading{bytes: out.DiskBytes, limit: out.DiskLimit, exact: out.DiskExact, at: time.Now()}
		diskCache.Store(user, r)
		cached = r
	}
	r := cached.(diskReading)
	out.DiskBytes, out.DiskLimit, out.DiskExact, out.DiskCheckedAt = r.bytes, r.limit, r.exact, r.at
	return out, nil
}
