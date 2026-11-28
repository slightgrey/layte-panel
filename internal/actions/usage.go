package actions

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
)

// AccountUsage is what a site login uses now, next to its limits, read from
// the kernel: the slice's cgroup (CPU, RAM, processes) and the disk quota
// (or du when quotas are off).
type AccountUsage struct {
	// Active: the login's slice is running (it has PHP or a cron job).
	Active      bool    `json:"active"`
	CPUPercent  float64 `json:"cpu_percent"`  // over the last half second, 100 = one CPU
	CPULimit    int     `json:"cpu_limit"`    // percent; 0 = none
	MemoryBytes int64   `json:"memory_bytes"` // RAM in use, page cache included (as the kernel limits it)
	MemoryLimit int64   `json:"memory_limit"` // bytes; 0 = none
	Processes   int     `json:"processes"`
	DiskBytes   int64   `json:"disk_bytes"`
	DiskLimit   int64   `json:"disk_limit"` // bytes; 0 = none
	// DiskExact: from the disk quota (instant). Otherwise measured with du.
	DiskExact bool `json:"disk_exact"`
}

// cgroupInt reads a cgroup file holding one number ("max" = 0, no limit).
func (d Deps) cgroupInt(dir, file string) int64 {
	b, err := d.FS.ReadFile(dir + "/" + file)
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return v // "max" parses as 0
}

// cgroupCPUUsec is cpu.stat's usage_usec.
func (d Deps) cgroupCPUUsec(dir string) (int64, bool) {
	b, err := d.FS.ReadFile(dir + "/cpu.stat")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "usage_usec "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// cpuLimit reads cpu.max ("<quota> <period>" or "max <period>") as a percent.
func (d Deps) cpuLimit(dir string) int {
	b, err := d.FS.ReadFile(dir + "/cpu.max")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) != 2 || f[0] == "max" {
		return 0
	}
	q, _ := strconv.ParseFloat(f[0], 64)
	p, _ := strconv.ParseFloat(f[1], 64)
	if p <= 0 {
		return 0
	}
	return int(q/p*100 + 0.5)
}

// AccountUsageParams: Disk also measures disk use (the quota, or du when
// quotas are off), the costly part; callers cache it.
type AccountUsageParams struct {
	Username string `json:"username"`
	Disk     bool   `json:"disk,omitempty"`
}

func (p AccountUsageParams) Validate() error { return UserParams{Username: p.Username}.Validate() }

func (d Deps) accountUsage(ctx context.Context, _ agent.Actor, p AccountUsageParams) (any, error) {
	if err := d.requireSiteUser(p.Username); err != nil {
		return nil, err
	}
	var u AccountUsage
	dir := acctCgroup(d.acctSlice(p.Username))
	if t0, ok := d.cgroupCPUUsec(dir); ok {
		u.Active = true
		start := time.Now()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		if t1, ok := d.cgroupCPUUsec(dir); ok {
			if el := time.Since(start).Microseconds(); el > 0 {
				u.CPUPercent = float64(t1-t0) / float64(el) * 100
			}
		}
		u.CPULimit = d.cpuLimit(dir)
		u.MemoryBytes = d.cgroupInt(dir, "memory.current")
		u.MemoryLimit = d.cgroupInt(dir, "memory.max")
		u.Processes = int(d.cgroupInt(dir, "pids.current"))
	}
	if !p.Disk {
		return u, nil
	}
	if q, ok := d.userQuota(ctx, p.Username); ok {
		u.DiskBytes, u.DiskLimit, u.DiskExact = q.Used, q.Limit, true
	} else {
		home := "/home/" + p.Username
		res, err := d.Runner.Run(ctx, duBin, "--summarize", "--block-size=1", "--one-file-system", "--", home)
		if sizes := parseDu(string(res.Stdout)); err == nil || len(sizes) > 0 {
			u.DiskBytes = sizes[home]
		}
	}
	return u, nil
}
