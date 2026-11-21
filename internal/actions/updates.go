package actions

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"layte-panel/internal/agent"
)

const apt = "/usr/bin/apt"

// updatesTTL: `apt list` is slow-ish and the answer rarely changes, so cache it.
const updatesTTL = 10 * time.Minute

// Updates is the result of updates.pending. It reflects the local package
// lists (as of the last `apt update`, usually run daily by unattended-upgrades).
type Updates struct {
	Total     int       `json:"total"`
	Security  int       `json:"security"`
	Packages  []Package `json:"packages"`
	CheckedAt time.Time `json:"checked_at"`
}

type Package struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	From     string `json:"from"`
	Security bool   `json:"security"`
}

type updates struct {
	deps Deps

	mu     sync.Mutex // guards cached/at; a Mutex lets only one goroutine in at a time
	cached *Updates
	at     time.Time
}

func (u *updates) pending(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := u.deps.Now()
	if u.cached != nil && now.Sub(u.at) < updatesTTL {
		return u.cached, nil
	}
	res, err := u.deps.Runner.Run(ctx, apt, "list", "--upgradable")
	if err != nil {
		return nil, fmt.Errorf("apt list: %w", err)
	}
	up := parseAptUpgradable(string(res.Stdout))
	up.CheckedAt = now.UTC()
	u.cached, u.at = &up, now
	return up, nil
}

// parseAptUpgradable parses lines like:
//
//	openssl/noble-updates,noble-security 3.0.13-0ubuntu3.4 amd64 [upgradable from: 3.0.13-0ubuntu3.1]
func parseAptUpgradable(out string) Updates {
	up := Updates{Packages: []Package{}}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "[upgradable from:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		name, suites, _ := strings.Cut(f[0], "/")
		p := Package{
			Name:     name,
			Version:  f[1],
			Security: strings.Contains(suites, "-security"),
		}
		if i := strings.Index(line, "[upgradable from: "); i >= 0 {
			p.From = strings.TrimSuffix(line[i+len("[upgradable from: "):], "]")
		}
		up.Packages = append(up.Packages, p)
		up.Total++
		if p.Security {
			up.Security++
		}
	}
	return up
}
