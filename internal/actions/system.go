package actions

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"layte-panel/internal/agent"
)

// SystemInfo is the result of system.info.
type SystemInfo struct {
	Hostname      string     `json:"hostname"`
	OS            string     `json:"os"`
	OSVersionID   string     `json:"os_version_id"`
	Kernel        string     `json:"kernel"`
	UptimeSeconds int64      `json:"uptime_seconds"`
	CPUs          int        `json:"cpus"`
	Load          [3]float64 `json:"load"`
	Memory        Memory     `json:"memory"`
	Disks         []Disk     `json:"disks"`
}

// Memory values are bytes.
type Memory struct {
	Total     uint64 `json:"total"`
	Available uint64 `json:"available"`
	Used      uint64 `json:"used"`
	SwapTotal uint64 `json:"swap_total"`
	SwapUsed  uint64 `json:"swap_used"`
}

type Disk struct {
	Path  string `json:"path"`
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
	Used  uint64 `json:"used"`
}

// diskPaths are reported on the dashboard. /var is shown separately only if
// it's a different filesystem.
var diskPaths = []string{"/", "/var", "/home"}

func (d Deps) systemInfo(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	var info SystemInfo
	read := func(name string) (string, error) {
		b, err := d.FS.ReadFile(name)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}
		return strings.TrimSpace(string(b)), nil
	}

	var err error
	if info.Hostname, err = read("/proc/sys/kernel/hostname"); err != nil {
		return nil, err
	}
	if info.Kernel, err = read("/proc/sys/kernel/osrelease"); err != nil {
		return nil, err
	}
	if s, err := read("/etc/os-release"); err == nil {
		osr := parseKeyValues(s)
		info.OS, info.OSVersionID = osr["PRETTY_NAME"], osr["VERSION_ID"]
	}
	if s, err := read("/proc/uptime"); err == nil {
		info.UptimeSeconds = parseUptime(s)
	}
	if s, err := read("/proc/loadavg"); err == nil {
		info.Load = parseLoadavg(s)
	}
	if s, err := read("/proc/cpuinfo"); err == nil {
		info.CPUs = countCPUs(s)
	}
	s, err := read("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	info.Memory = parseMeminfo(s)

	info.Disks = []Disk{}
	seen := map[[2]uint64]bool{}
	for _, p := range diskPaths {
		u, err := d.FS.Statfs(p)
		if err != nil {
			continue // path may not exist (e.g. no separate /home)
		}
		key := [2]uint64{u.Total, u.Free}
		if seen[key] {
			continue // same filesystem as an earlier path
		}
		seen[key] = true
		info.Disks = append(info.Disks, Disk{Path: p, Total: u.Total, Free: u.Free, Used: u.Total - u.Free})
	}
	return info, nil
}

// parseKeyValues parses KEY=value / KEY="value" lines (os-release, systemctl show).
func parseKeyValues(s string) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		m[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return m
}

func parseUptime(s string) int64 {
	f, err := strconv.ParseFloat(strings.Fields(s + " 0")[0], 64)
	if err != nil {
		return 0
	}
	return int64(f)
}

func parseLoadavg(s string) [3]float64 {
	var out [3]float64
	f := strings.Fields(s)
	for i := 0; i < 3 && i < len(f); i++ {
		out[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return out
}

func countCPUs(s string) int {
	n := 0
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if k, _, ok := strings.Cut(sc.Text(), ":"); ok && strings.TrimSpace(k) == "processor" {
			n++
		}
	}
	return n
}

// parseMeminfo reads /proc/meminfo (values in kB) into bytes.
func parseMeminfo(s string) Memory {
	kv := map[string]uint64{}
	sc := bufio.NewScanner(bytes.NewReader([]byte(s)))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		kv[k] = n * 1024
	}
	m := Memory{
		Total:     kv["MemTotal"],
		Available: kv["MemAvailable"],
		SwapTotal: kv["SwapTotal"],
	}
	if m.Total > m.Available {
		m.Used = m.Total - m.Available
	}
	if kv["SwapTotal"] > kv["SwapFree"] {
		m.SwapUsed = kv["SwapTotal"] - kv["SwapFree"]
	}
	return m
}
