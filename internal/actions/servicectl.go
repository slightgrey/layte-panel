package actions

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// Reloading and restarting services, and rebooting the server, from the
// panel. A fixed table says which service may get which action: a reload
// where the service has a safe one (after its config test, so a broken
// config changes nothing), a restart only where no reload exists. Layte's
// own services get none (the agent would cut off its own action).

// ServiceOp is the one action a service offers.
type ServiceOp struct {
	Op string `json:"op"` // reload | restart
	// Disruptive: a restart that interrupts every site for a moment (MySQL).
	Disruptive bool `json:"disruptive,omitempty"`
	test       *system.Cmd
}

var (
	phpFPMUnitRe  = regexp.MustCompile(`^php([0-9]+\.[0-9]+)-fpm$`)
	acctFPMUnitRe = regexp.MustCompile(`^layte-fpm-([a-z][a-z0-9_-]{2,31})-([0-9]+\.[0-9]+)$`)
	acctRedisRe   = regexp.MustCompile(`^layte-redis@([a-z][a-z0-9_-]{2,31})$`)
)

// serviceOp is the table: the action the named service (without .service)
// offers, or false.
func serviceOp(name string) (ServiceOp, bool) {
	switch name {
	case "nginx":
		return ServiceOp{Op: "reload", test: &nginxTest}, true
	case "ssh":
		return ServiceOp{Op: "reload", test: &sshdTest}, true
	case "cron", "sshguard", "redis-server":
		return ServiceOp{Op: "restart"}, true
	case "mysql", "mariadb":
		return ServiceOp{Op: "restart", Disruptive: true}, true
	}
	if m := phpFPMUnitRe.FindStringSubmatch(name); m != nil {
		t := fpmTest(m[1])
		return ServiceOp{Op: "reload", test: &t}, true
	}
	if m := acctFPMUnitRe.FindStringSubmatch(name); m != nil && validate.SiteUsername(m[1]) == nil {
		t := system.Cmd{Bin: phpFPMBin(m[2]), Args: []string{"-t", "--fpm-config", acctFPMConf(m[1], m[2])}}
		return ServiceOp{Op: "reload", test: &t}, true
	}
	if m := acctRedisRe.FindStringSubmatch(name); m != nil && validate.SiteUsername(m[1]) == nil {
		return ServiceOp{Op: "restart"}, true
	}
	return ServiceOp{}, false // ufw (its rules are on the Security page), layte, layte-agent, …
}

// ServiceDetail is a row of the Services page.
type ServiceDetail struct {
	Service
	Since       *time.Time `json:"since,omitempty"`        // active since (nil: unknown)
	MemoryBytes int64      `json:"memory_bytes,omitempty"` // the unit's cgroup
	Action      *ServiceOp `json:"action,omitempty"`
}

// servicesDetail lists the server's services (the dashboard's, plus Redis,
// sshguard and the site logins' own PHP and Redis) with what each uses and
// the action it offers.
func (d Deps) servicesDetail(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	units := append([]string{}, coreServices...)
	units = append(units, "redis-server", "sshguard", "layte", "layte-agent")
	php, err := d.phpVersions()
	if err != nil {
		return nil, err
	}
	for _, v := range php {
		units = append(units, "php"+v+"-fpm")
	}
	own, _ := d.FS.Glob(systemdUnitDir + "/layte-fpm-*.service")
	redis, _ := d.FS.Glob(systemdUnitDir + "/multi-user.target.wants/layte-redis@*.service")
	for _, p := range append(own, redis...) {
		units = append(units, strings.TrimSuffix(path.Base(p), ".service"))
	}
	args := []string{"show", "--no-pager", "--property=Id,Description,LoadState,ActiveState,SubState,UnitFileState,ActiveEnterTimestamp,MemoryCurrent"}
	for _, u := range units {
		args = append(args, u+".service")
	}
	res, err := d.Runner.Run(ctx, systemctl, args...)
	if err != nil {
		return nil, fmt.Errorf("systemctl show: %w", err)
	}
	out := []ServiceDetail{}
	seen := map[string]bool{}
	for _, block := range strings.Split(strings.ReplaceAll(string(res.Stdout), "\r\n", "\n"), "\n\n") {
		kv := parseKeyValues(block)
		id := kv["Id"]
		if id == "" || seen[id] || kv["LoadState"] == "not-found" {
			continue
		}
		seen[id] = true
		s := ServiceDetail{Service: Service{
			Name: strings.TrimSuffix(id, ".service"), Description: kv["Description"], Installed: true,
			Active: kv["ActiveState"], Sub: kv["SubState"], Enabled: kv["UnitFileState"],
		}}
		if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", kv["ActiveEnterTimestamp"]); err == nil && s.Active == "active" {
			s.Since = &t
		}
		if n, err := strconv.ParseInt(kv["MemoryCurrent"], 10, 64); err == nil && n > 0 && n < 1<<50 {
			s.MemoryBytes = n
		}
		if op, ok := serviceOp(s.Name); ok {
			s.Action = &op
		}
		out = append(out, s)
	}
	return out, nil
}

// ServiceControlParams asks for a service's action.
type ServiceControlParams struct {
	Name string `json:"name"` // without .service
	Op   string `json:"op"`   // reload | restart
}

func (p ServiceControlParams) Validate() error {
	op, ok := serviceOp(p.Name)
	if !ok {
		return fmt.Errorf("%s can't be reloaded or restarted from the panel", p.Name)
	}
	if op.Op != p.Op {
		return fmt.Errorf("%s offers %s, not %s", p.Name, op.Op, p.Op)
	}
	return nil
}

// serviceControl runs the service's config test (a failing one stops here,
// nothing changes), then the reload or restart, and reports its state.
func (d Deps) serviceControl(ctx context.Context, _ agent.Actor, p ServiceControlParams) (any, error) {
	op, _ := serviceOp(p.Name)
	unit := p.Name + ".service"
	if res, err := d.Runner.Run(ctx, systemctl, "cat", unit); err != nil {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s isn't installed: %s", p.Name, failure(res, err, 2))
	}
	if op.test != nil {
		if res, err := d.Runner.RunCmd(ctx, *op.test); err != nil {
			return nil, agent.Errorf("config_test_failed", "%s's config test failed, so it wasn't %sed: %s", p.Name, p.Op, failure(res, err, 5))
		}
	}
	if res, err := d.Runner.Run(ctx, systemctl, p.Op, unit); err != nil {
		return nil, fmt.Errorf("systemctl %s %s: %s", p.Op, unit, failure(res, err, 5))
	}
	res, _ := d.Runner.Run(ctx, systemctl, "is-active", unit)
	return map[string]string{"name": p.Name, "active": strings.TrimSpace(string(res.Stdout))}, nil
}

// Reboot.

const (
	shutdownBin        = "/usr/sbin/shutdown"
	shutdownScheduled  = "/run/systemd/shutdown/scheduled"
	rebootDelayMinutes = 1
)

// RebootStatus: whether a reboot is needed (updates) and whether one is
// scheduled.
type RebootStatus struct {
	Required  bool       `json:"required"`
	Packages  []string   `json:"packages,omitempty"`
	Scheduled *time.Time `json:"scheduled,omitempty"`
	Hostname  string     `json:"hostname"`
}

func (d Deps) rebootStatus(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	var st RebootStatus
	st.Required, _ = d.FS.Exists(rebootFlagPath)
	if b, err := d.FS.ReadFile(rebootPkgsPath); err == nil {
		for _, p := range strings.Fields(string(b)) {
			if !slices.Contains(st.Packages, p) {
				st.Packages = append(st.Packages, p)
			}
		}
	}
	st.Scheduled = d.scheduledReboot(ctx)
	if b, err := d.FS.ReadFile("/proc/sys/kernel/hostname"); err == nil {
		st.Hostname = strings.TrimSpace(string(b))
	}
	return st, nil
}

const busctlBin = "/usr/bin/busctl"

var scheduledShutdownRe = regexp.MustCompile(`^\(st\) "([a-z-]*)" ([0-9]+)\s*$`)

// scheduledReboot asks systemd-logind (the source of truth: its schedule
// file isn't always there) when a reboot is scheduled. Without busctl (dev
// mode) it reads the schedule file.
func (d Deps) scheduledReboot(ctx context.Context) *time.Time {
	var mode string
	var usec int64
	res, err := d.Runner.Run(ctx, busctlBin, "get-property", "org.freedesktop.login1", "/org/freedesktop/login1", "org.freedesktop.login1.Manager", "ScheduledShutdown")
	if m := scheduledShutdownRe.FindStringSubmatch(strings.TrimSpace(string(res.Stdout))); err == nil && m != nil {
		mode = m[1]
		usec, _ = strconv.ParseInt(m[2], 10, 64) // "none" is 2^64-1: doesn't parse, stays 0
	} else if b, err := d.FS.ReadFile(shutdownScheduled); err == nil {
		kv := parseKeyValues(string(b))
		mode = kv["MODE"]
		usec, _ = strconv.ParseInt(kv["USEC"], 10, 64)
	}
	// Past by minutes means it didn't happen; don't show it as on its way.
	if mode != "reboot" || usec <= 0 || time.Since(time.UnixMicro(usec)) > 2*time.Minute {
		return nil
	}
	t := time.UnixMicro(usec)
	return &t
}

// systemReboot schedules a reboot in a minute (so the panel can answer and
// it can still be cancelled). Outside the agent's sandbox, like apt.
func (d Deps) systemReboot(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	// logind's own API (what shutdown -r +1 uses), so the schedule and its
	// cancel go to the same place.
	at := time.Now().Add(rebootDelayMinutes * time.Minute).UnixMicro()
	res, err := d.Runner.Run(ctx, busctlBin, "call", "org.freedesktop.login1", "/org/freedesktop/login1", "org.freedesktop.login1.Manager",
		"ScheduleShutdown", "st", "reboot", strconv.FormatInt(at, 10))
	if err != nil {
		return nil, fmt.Errorf("schedule the reboot: %s", failure(res, err, 3))
	}
	return d.rebootStatus(ctx, agent.Actor{}, agent.NoParams{})
}

// systemRebootCancel cancels a scheduled reboot.
func (d Deps) systemRebootCancel(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	if d.scheduledReboot(ctx) == nil {
		return nil, errors.New("no reboot is scheduled")
	}
	res, err := d.Runner.Run(ctx, busctlBin, "call", "org.freedesktop.login1", "/org/freedesktop/login1", "org.freedesktop.login1.Manager", "CancelScheduledShutdown")
	if err != nil {
		return nil, fmt.Errorf("cancel the reboot: %s", failure(res, err, 3))
	}
	// A moment before logind reports it gone.
	for i := 0; i < 20 && d.scheduledReboot(ctx) != nil; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return d.rebootStatus(ctx, agent.Actor{}, agent.NoParams{})
}
