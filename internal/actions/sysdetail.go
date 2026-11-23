package actions

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

// Read-only breakdowns behind the dashboard's Load, Memory and Disk cards.

// ---- processes (CPU + memory) ----

// cpuSample is how long CPU use is measured (tests make it shorter).
var cpuSample = 500 * time.Millisecond

// MemDetail is /proc/meminfo split into what the memory page shows.
type MemDetail struct {
	Total        uint64 `json:"total"`
	Available    uint64 `json:"available"`
	Free         uint64 `json:"free"`
	Buffers      uint64 `json:"buffers"`
	Cached       uint64 `json:"cached"` // page cache + reclaimable slab
	Shmem        uint64 `json:"shmem"`
	SwapTotal    uint64 `json:"swap_total"`
	SwapFree     uint64 `json:"swap_free"`
	AppsUsed     uint64 `json:"apps_used"` // total - available
	ProcessesRSS uint64 `json:"processes_rss"`
}

// ProcInfo is one process.
type ProcInfo struct {
	PID     int     `json:"pid"`
	User    string  `json:"user"`
	Name    string  `json:"name"`
	Command string  `json:"command"` // arguments, secrets masked, shortened
	Group   string  `json:"group"`   // what it belongs to (service, PHP pool, …)
	RSS     uint64  `json:"rss"`     // bytes in RAM
	CPU     float64 `json:"cpu"`     // % of the whole machine during the sample
}

// ProcGroup sums processes that belong together.
type ProcGroup struct {
	Name      string  `json:"name"`
	Processes int     `json:"processes"`
	RSS       uint64  `json:"rss"`
	CPU       float64 `json:"cpu"`
}

type ProcessesResult struct {
	CPUs     int         `json:"cpus"`
	Load     [3]float64  `json:"load"`
	Uptime   int64       `json:"uptime_seconds"`
	Memory   MemDetail   `json:"memory"`
	Groups   []ProcGroup `json:"groups"`
	Procs    []ProcInfo  `json:"processes"`
	SampleMS int64       `json:"sample_ms"`
}

var (
	// Secrets on a command line: --password=X / --password X, password=X,
	// MYSQL_PWD=X, token=X, secret=X; and -pX for the MySQL tools.
	secretArgRe = regexp.MustCompile(`(?i)(--password[= ]|\bpassword=|\bmysql_pwd=|\btoken=|\bsecret=)(\S+)`)
	mysqlPRe    = regexp.MustCompile(`(\s-p)(\S+)`)
	mysqlToolRe = regexp.MustCompile(`(^|/)(mysql|mysqldump|mysqladmin|mariadb|mariadb-dump)(\s|$)`)
	unitRe      = regexp.MustCompile(`([^/]+\.(?:service|scope))$`)
)

func maskSecrets(cmd string) string {
	cmd = secretArgRe.ReplaceAllString(cmd, "${1}***")
	if mysqlToolRe.MatchString(cmd) {
		cmd = mysqlPRe.ReplaceAllString(cmd, "${1}***")
	}
	return cmd
}

type procSample struct {
	ProcInfo
	ticks uint64
}

// readProcs reads every process from /proc (kernel threads skipped).
func (d Deps) readProcs(users map[int]string) map[int]procSample {
	out := map[int]procSample{}
	dirs, _ := d.FS.Glob("/proc/[0-9]*")
	for _, dir := range dirs {
		pid, err := strconv.Atoi(path.Base(dir))
		if err != nil {
			continue
		}
		status, err := d.FS.ReadFile(dir + "/status")
		if err != nil {
			continue // exited
		}
		p := procSample{ProcInfo: ProcInfo{PID: pid}}
		for _, line := range strings.Split(string(status), "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch k {
			case "Name":
				p.Name = v
			case "Uid":
				if f := strings.Fields(v); len(f) > 0 {
					uid, _ := strconv.Atoi(f[0])
					p.User = users[uid]
					if p.User == "" {
						p.User = f[0]
					}
				}
			case "VmRSS":
				if f := strings.Fields(v); len(f) > 0 {
					kb, _ := strconv.ParseUint(f[0], 10, 64)
					p.RSS = kb * 1024
				}
			}
		}
		cmd, _ := d.FS.ReadFile(dir + "/cmdline")
		if len(cmd) == 0 && p.RSS == 0 {
			continue // kernel thread
		}
		p.Command = maskSecrets(strings.TrimSpace(strings.ReplaceAll(string(cmd), "\x00", " ")))
		if len(p.Command) > 300 {
			p.Command = p.Command[:300] + "…"
		}
		if st, err := d.FS.ReadFile(dir + "/stat"); err == nil {
			s := string(st)
			if i := strings.LastIndexByte(s, ')'); i >= 0 {
				f := strings.Fields(s[i+1:])
				if len(f) > 12 { // utime and stime are fields 14 and 15 of the whole line
					u, _ := strconv.ParseUint(f[11], 10, 64)
					sy, _ := strconv.ParseUint(f[12], 10, 64)
					p.ticks = u + sy
				}
			}
		}
		cg, _ := d.FS.ReadFile(dir + "/cgroup")
		p.Group = procGroup(p.Name, p.Command, string(cg))
		out[pid] = p
	}
	return out
}

// procGroup names what a process belongs to: its PHP-FPM pool, else its
// systemd unit, else its name.
func procGroup(name, cmd, cgroup string) string {
	if pool, ok := strings.CutPrefix(cmd, "php-fpm: pool "); ok {
		return "PHP pool " + strings.TrimSpace(pool)
	}
	if strings.HasPrefix(cmd, "php-fpm: master process") {
		return "PHP-FPM master"
	}
	for _, line := range strings.Split(cgroup, "\n") {
		if m := unitRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			unit := m[1]
			if strings.HasPrefix(unit, "session-") {
				return "SSH/login sessions"
			}
			if unit == "init.scope" {
				return "systemd"
			}
			return strings.TrimSuffix(unit, ".service")
		}
	}
	return name
}

// totalTicks is the sum of the "cpu" line in /proc/stat (all CPUs).
func (d Deps) totalTicks() uint64 {
	b, err := d.FS.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line, _, _ := strings.Cut(string(b), "\n")
	var sum uint64
	for _, f := range strings.Fields(line)[1:] {
		n, _ := strconv.ParseUint(f, 10, 64)
		sum += n
	}
	return sum
}

func (d Deps) userNames() map[int]string {
	out := map[int]string{}
	b, _ := d.FS.ReadFile("/etc/passwd")
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) > 2 {
			if uid, err := strconv.Atoi(f[2]); err == nil {
				out[uid] = f[0]
			}
		}
	}
	return out
}

func parseMeminfoKB(s string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if f := strings.Fields(v); len(f) > 0 {
			n, _ := strconv.ParseUint(f[0], 10, 64)
			out[k] = n * 1024
		}
	}
	return out
}

// systemProcesses samples every process twice, cpuSample apart (read-only).
func (d Deps) systemProcesses(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	users := d.userNames()
	t0, first := d.totalTicks(), d.readProcs(users)
	start := time.Now()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(cpuSample):
	}
	t1, second := d.totalTicks(), d.readProcs(users)
	elapsed := time.Since(start)

	res := ProcessesResult{Procs: []ProcInfo{}, Groups: []ProcGroup{}, SampleMS: elapsed.Milliseconds()}
	if b, err := d.FS.ReadFile("/proc/cpuinfo"); err == nil {
		res.CPUs = countCPUs(string(b))
	}
	if b, err := d.FS.ReadFile("/proc/loadavg"); err == nil {
		res.Load = parseLoadavg(strings.TrimSpace(string(b)))
	}
	if b, err := d.FS.ReadFile("/proc/uptime"); err == nil {
		res.Uptime = parseUptime(strings.TrimSpace(string(b)))
	}
	if b, err := d.FS.ReadFile("/proc/meminfo"); err == nil {
		m := parseMeminfoKB(string(b))
		res.Memory = MemDetail{
			Total: m["MemTotal"], Available: m["MemAvailable"], Free: m["MemFree"], Buffers: m["Buffers"],
			Cached: m["Cached"] + m["SReclaimable"], Shmem: m["Shmem"], SwapTotal: m["SwapTotal"], SwapFree: m["SwapFree"],
		}
		if res.Memory.Total > res.Memory.Available {
			res.Memory.AppsUsed = res.Memory.Total - res.Memory.Available
		}
	}

	groups := map[string]*ProcGroup{}
	for pid, p := range second {
		if old, ok := first[pid]; ok && t1 > t0 && p.ticks >= old.ticks {
			p.CPU = float64(p.ticks-old.ticks) / float64(t1-t0) * 100
		}
		res.Memory.ProcessesRSS += p.RSS
		res.Procs = append(res.Procs, p.ProcInfo)
		g := groups[p.Group]
		if g == nil {
			g = &ProcGroup{Name: p.Group}
			groups[p.Group] = g
		}
		g.Processes++
		g.RSS += p.RSS
		g.CPU += p.CPU
	}
	for _, g := range groups {
		res.Groups = append(res.Groups, *g)
	}
	sort.Slice(res.Procs, func(i, j int) bool { return res.Procs[i].RSS > res.Procs[j].RSS })
	sort.Slice(res.Groups, func(i, j int) bool { return res.Groups[i].RSS > res.Groups[j].RSS })
	return res, nil
}

// ---- disk breakdown ----

const findBin = "/usr/bin/find"

// DiskBreakdownParams: the folder to break down. Mode "files" lists the
// largest files under it instead of its subfolders.
type DiskBreakdownParams struct {
	Path string `json:"path"`
	Mode string `json:"mode,omitempty"` // "" (folders) | "files"
}

func (p DiskBreakdownParams) Validate() error {
	if !strings.HasPrefix(p.Path, "/") || path.Clean(p.Path) != p.Path || strings.ContainsRune(p.Path, 0) || len(p.Path) > 1024 {
		return fmt.Errorf("path must be a clean absolute path")
	}
	if p.Mode != "" && p.Mode != "files" {
		return fmt.Errorf("unknown mode %q", p.Mode)
	}
	return nil
}

// DiskEntry is a folder or file and its size on disk.
type DiskEntry struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type DiskBreakdown struct {
	Path    string      `json:"path"`
	Total   int64       `json:"total"`             // folders mode: the folder itself
	Entries []DiskEntry `json:"entries"`           // largest first
	Partial bool        `json:"partial,omitempty"` // some paths couldn't be read
}

// largeFile is the size from which "files" mode lists a file.
const largeFile = "20M"

// diskBreakdown sizes a folder's direct subfolders (du, one level, this
// filesystem only), or finds its largest files. Read-only.
func (d Deps) diskBreakdown(ctx context.Context, _ agent.Actor, p DiskBreakdownParams) (any, error) {
	fi, err := d.FS.Lstat(p.Path)
	if err != nil || !fi.IsDir() {
		return nil, agent.Errorf(agent.CodeNotFound, "%s is not a folder", p.Path)
	}
	out := DiskBreakdown{Path: p.Path, Entries: []DiskEntry{}}
	var res system.Result
	if p.Mode == "files" {
		res, err = d.Runner.Run(ctx, findBin, p.Path, "-xdev", "-type", "f", "-size", "+"+largeFile, "-printf", `%s\t%p\n`)
	} else {
		res, err = d.Runner.Run(ctx, duBin, "--one-file-system", "--max-depth=1", "--block-size=1", "--", p.Path)
	}
	var ee *system.ExitError
	if err != nil {
		if !errors.As(err, &ee) || len(res.Stdout) == 0 {
			return nil, agent.Errorf("scan_failed", "%s", failure(res, err, 3))
		}
		out.Partial = true // permission denied / vanished files: what we have is still useful
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		n, pth, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		b, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		if err != nil {
			continue
		}
		if p.Mode != "files" && pth == p.Path {
			out.Total = b
			continue
		}
		out.Entries = append(out.Entries, DiskEntry{Path: pth, Bytes: b})
	}
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].Bytes > out.Entries[j].Bytes })
	if p.Mode == "files" && len(out.Entries) > 50 {
		out.Entries = out.Entries[:50]
	}
	return out, nil
}
