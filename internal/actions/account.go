package actions

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// Resource limits for a site login (one Linux user).
//
//   - A systemd slice, layte-acct-<user>.slice, carries the limits:
//     CPUQuota (percent of one CPU), MemoryMax (no swap past it) and a
//     process cap. The kernel enforces them for everything in the slice.
//   - The login's PHP runs in its own PHP-FPM master per PHP version,
//     layte-fpm-<user>-<version>.service, inside that slice. Its pools live
//     in /etc/layte/fpm/<user>/<version>/pool.d/ and listen in
//     /run/layte-fpm/<user>/, so one login's PHP never shares a master
//     (or its limits) with another's.
//   - Its cron jobs run in the slice too, through `layte cron-exec`.
//
// Sites without a login keep the shared php<version>-fpm services.

const (
	acctFPMRoot   = "/etc/layte/fpm"
	acctRunRoot   = "/run/layte-fpm"
	acctTasksMax  = 256
	maxCPUPercent = 800 // eight CPUs' worth
	maxMemoryMB   = 65536
	minMemoryMB   = 64
)

// AccountSlice is the login's slice, named by its Linux user's UID: a "-"
// in a slice name means nesting to systemd (layte-acct-my-site.slice would
// sit inside layte-acct-my.slice, under the login "my"'s limits), and a UID
// has none.
func AccountSlice(uid uint32) string { return fmt.Sprintf("layte-acct-u%d.slice", uid) }

// acctSlice is user's slice ("" if there's no such user).
func (d Deps) acctSlice(user string) string {
	cred, err := d.Users.Lookup(user)
	if err != nil {
		return ""
	}
	return AccountSlice(cred.UID)
}

// acctCgroup is a slice's cgroup folder (layte.slice/layte-acct.slice/…).
func acctCgroup(slice string) string {
	return "/sys/fs/cgroup/layte.slice/layte-acct.slice/" + slice
}

// acctFPMUnits lists the user's own PHP-FPM units by version. The version
// must be the whole rest of the name: "layte-fpm-my-*" also matches the
// login "my-site"'s units, which must never be touched for "my".
func (d Deps) acctFPMUnits(user string) map[string]string {
	prefix := "layte-fpm-" + user + "-"
	out := map[string]string{}
	paths, _ := d.FS.Glob(systemdUnitDir + "/" + prefix + "*.service")
	for _, p := range paths {
		v := strings.TrimSuffix(strings.TrimPrefix(path.Base(p), prefix), ".service")
		if phpVersionRe.MatchString(v) {
			out[v] = path.Base(p)
		}
	}
	return out
}

func acctFPMUnit(user, version string) string {
	return "layte-fpm-" + user + "-" + version + ".service"
}

func acctFPMDir(user, version string) string { return acctFPMRoot + "/" + user + "/" + version }
func acctFPMConf(user, version string) string {
	return acctFPMDir(user, version) + "/php-fpm.conf"
}

// AccountParams are a site login's limits; 0 means no limit.
type AccountParams struct {
	Username   string `json:"username"`
	CPUPercent int    `json:"cpu_percent"`
	MemoryMB   int    `json:"memory_mb"`
}

func (p AccountParams) Validate() error {
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	if p.CPUPercent < 0 || p.CPUPercent > maxCPUPercent {
		return fmt.Errorf("CPU limit must be 0-%d%%", maxCPUPercent)
	}
	if p.MemoryMB != 0 && (p.MemoryMB < minMemoryMB || p.MemoryMB > maxMemoryMB) {
		return fmt.Errorf("memory limit must be %d-%d MB (or 0 for none)", minMemoryMB, maxMemoryMB)
	}
	return nil
}

func sliceContent(p AccountParams) string {
	var b strings.Builder
	b.WriteString(system.Marker + " — do not edit by hand\n")
	fmt.Fprintf(&b, "[Unit]\nDescription=Layte Panel limits for %s\n\n[Slice]\n", p.Username)
	if p.CPUPercent > 0 {
		fmt.Fprintf(&b, "CPUQuota=%d%%\n", p.CPUPercent)
	}
	if p.MemoryMB > 0 {
		fmt.Fprintf(&b, "MemoryMax=%dM\nMemorySwapMax=0\n", p.MemoryMB)
	}
	fmt.Fprintf(&b, "TasksMax=%d\n", acctTasksMax)
	return b.String()
}

var daemonReload = system.Cmd{Bin: systemctl, Args: []string{"daemon-reload"}}

// requireSiteUser: limits only go to users Layte created for sites.
func (d Deps) requireSiteUser(user string) error {
	if _, err := d.Users.Lookup(user); err != nil {
		return err
	}
	if ok, _ := d.FS.Exists("/home/" + user + "/" + siteUserMarker); !ok {
		return agent.Errorf(agent.CodeInvalidParams, "%s is not a Layte site user", user)
	}
	return nil
}

// accountApply writes (or updates) the login's slice. Changed limits apply
// at once to everything already running in it.
func (d Deps) accountApply(ctx context.Context, _ agent.Actor, p AccountParams) (any, error) {
	if err := d.requireSiteUser(p.Username); err != nil {
		return nil, err
	}
	if err := d.FS.MkdirAll(systemdUnitDir, 0o755); err != nil {
		return nil, err
	}
	slice := d.acctSlice(p.Username)
	if slice == "" {
		return nil, agent.Errorf(agent.CodeInvalidParams, "no user %s", p.Username)
	}
	if err := system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{
		Path: systemdUnitDir + "/" + slice, Content: []byte(sliceContent(p)), Reload: daemonReload,
	}); err != nil {
		return nil, err
	}
	return map[string]string{"slice": slice}, nil
}

// accountRemove removes the login's slice once its sites are back on the
// shared PHP-FPM services (the caller moves them first).
func (d Deps) accountRemove(ctx context.Context, _ agent.Actor, p UserParams) (any, error) {
	for _, unit := range d.acctFPMUnits(p.Username) {
		if err := d.stopAccountFPM(ctx, unit); err != nil {
			return nil, err
		}
	}
	d.FS.RemoveAll(acctFPMRoot + "/" + p.Username)
	slice := d.acctSlice(p.Username)
	if slice == "" { // no such user (any more): its slice can't be named
		return map[string]bool{"removed": false}, nil
	}
	unitPath := systemdUnitDir + "/" + slice
	if ok, _ := d.FS.Exists(unitPath); ok {
		if err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{Path: unitPath, Test: daemonReload, Reload: daemonReload}); err != nil {
			return nil, err
		}
	}
	return map[string]bool{"removed": true}, nil
}

func fpmMasterContent(user, version string) string {
	return system.MarkerINI + ` — do not edit by hand
; PHP ` + version + ` for the site login ` + user + ` (Layte Panel). Its pools are in pool.d/.
[global]
pid = ` + acctRunRoot + `/` + user + `/php` + version + `.pid
error_log = syslog
syslog.ident = layte-fpm-` + user + `-` + version + `
daemonize = no
; Reloads wait for running requests (like Layte's shared pools).
process_control_timeout = 10s
include = ` + acctFPMDir(user, version) + `/pool.d/*.conf
`
}

func fpmUnitContent(user, version, slice string) string {
	return system.Marker + ` — do not edit by hand
[Unit]
Description=PHP ` + version + ` FPM for ` + user + ` (Layte Panel)
After=network.target

[Service]
Type=notify
Slice=` + slice + `
RuntimeDirectory=layte-fpm/` + user + `
RuntimeDirectoryPreserve=yes
ExecStart=` + phpFPMBin(version) + ` --nodaemonize --fpm-config ` + acctFPMConf(user, version) + `
ExecReload=/bin/kill -USR2 $MAINPID
Restart=on-failure
# A request over the RAM limit gets its worker killed by the kernel; the
# master keeps running and starts a new one (systemd's default would stop
# the whole service, taking every site of the login down).
OOMPolicy=continue

[Install]
WantedBy=multi-user.target
`
}

// ensureAccountFPM installs the login's PHP-FPM master for one version (not
// started yet: it needs a pool first).
func (d Deps) ensureAccountFPM(ctx context.Context, user, version string) error {
	if err := d.FS.MkdirAll(acctFPMDir(user, version)+"/pool.d", 0o755); err != nil {
		return err
	}
	if err := d.FS.WriteFileAtomic(acctFPMConf(user, version), []byte(fpmMasterContent(user, version)), 0o644); err != nil {
		return err
	}
	if ok, _ := d.FS.Exists(systemdUnitDir + "/" + d.acctSlice(user)); !ok {
		// No limits set yet: an empty slice, so the unit's Slice= exists.
		if _, err := d.accountApply(ctx, agent.Actor{}, AccountParams{Username: user}); err != nil {
			return err
		}
	}
	unit := acctFPMUnit(user, version)
	if err := system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{
		Path: systemdUnitDir + "/" + unit, Content: []byte(fpmUnitContent(user, version, d.acctSlice(user))), Reload: daemonReload,
	}); err != nil {
		return err
	}
	if res, err := d.Runner.Run(ctx, systemctl, "enable", unit); err != nil {
		return fmt.Errorf("enable %s: %s", unit, failure(res, err, 3))
	}
	return nil
}

// stopAccountFPM stops and removes one of the login's PHP-FPM masters.
func (d Deps) stopAccountFPM(ctx context.Context, unit string) error {
	unitPath := systemdUnitDir + "/" + unit
	if ok, _ := d.FS.Exists(unitPath); !ok {
		return nil
	}
	if res, err := d.Runner.Run(ctx, systemctl, "disable", "--now", unit); err != nil {
		return fmt.Errorf("stop %s: %s", unit, failure(res, err, 3))
	}
	return system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{Path: unitPath, Test: daemonReload, Reload: daemonReload})
}

// poolTarget is where a site's pool lives and how it is tested and reloaded:
// the shared php<version>-fpm, or the login's own master.
type poolTarget struct {
	path         string
	test, reload system.Cmd
}

func accountPoolTarget(user, version, domain string) poolTarget {
	conf := acctFPMConf(user, version)
	return poolTarget{
		path:   templates.AccountPoolPath(user, version, domain),
		test:   system.Cmd{Bin: phpFPMBin(version), Args: []string{"-t", "--fpm-config", conf}},
		reload: system.Cmd{Bin: systemctl, Args: []string{"reload-or-restart", acctFPMUnit(user, version)}},
	}
}

func sharedPoolTarget(version, domain string) poolTarget {
	return poolTarget{path: templates.PoolPath(version, domain), test: fpmTest(version), reload: fpmReload(version)}
}

// removeAccountPool removes a pool from the login's master; the last one
// stops the master (php-fpm refuses to run without a pool).
func (d Deps) removeAccountPool(ctx context.Context, user, version, domain string) (bool, error) {
	t := accountPoolTarget(user, version, domain)
	if ok, _ := d.FS.Exists(t.path); !ok {
		return false, nil
	}
	pools, _ := d.FS.Glob(acctFPMDir(user, version) + "/pool.d/*.conf")
	if len(pools) <= 1 {
		if err := d.stopAccountFPM(ctx, acctFPMUnit(user, version)); err != nil {
			return false, err
		}
		if err := d.FS.Remove(t.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		return true, nil
	}
	if err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{Path: t.path, Test: t.test, Reload: t.reload}); err != nil {
		return false, configError(err)
	}
	return true, nil
}
