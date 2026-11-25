package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

// Installing all pending updates (not only security ones). Safety rules:
//   - only `apt-get upgrade`: it never removes packages and never installs
//     new ones (never full-upgrade / dist-upgrade / do-release-upgrade);
//   - kept-back packages only on explicit request (`install --only-upgrade
//     <those packages>`), and only if nothing would be removed;
//   - existing config files are always kept (--force-confold), so configs
//     the admin (or Layte) wrote are never replaced by a package's version;
//   - the admin confirms a preview; the run refuses if the plan changed since;
//   - non-interactive, waits for apt's lock instead of failing;
//   - afterwards: services restarted by needrestart, every site checked,
//     reboot need reported (Layte never reboots by itself).

const (
	aptGetBin       = "/usr/bin/apt-get"
	dpkgBin         = "/usr/bin/dpkg"
	needrestartBin  = "/usr/sbin/needrestart"
	rebootFlagPath  = "/run/reboot-required"
	rebootPkgsPath  = "/run/reboot-required.pkgs"
	aptLockWaitSecs = "300"
)

var pkgNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9+.\-]{0,99}(:[a-z0-9]+)?$`)

// PlannedPkg is one package apt would install or upgrade.
type PlannedPkg struct {
	Name     string `json:"name"`
	From     string `json:"from,omitempty"` // installed version ("" = new package)
	To       string `json:"to"`
	Origin   string `json:"origin"`
	Security bool   `json:"security"`
}

// UpgradePlan is what an upgrade would do (from apt-get --simulate).
type UpgradePlan struct {
	Upgrades    []PlannedPkg `json:"upgrades"`
	NewPackages []PlannedPkg `json:"new_packages"` // dependencies a kept-back install pulls in
	Removals    []string     `json:"removals"`     // must be empty to proceed
	KeptBack    []string     `json:"kept_back"`
	// Phased: Ubuntu releases these updates to a growing share of servers
	// over a few days, and this server's turn hasn't come yet, so a normal
	// upgrade skips them. Installing them by name ("install now anyway")
	// still works.
	Phased       []string `json:"phased"`
	Restarts     []string `json:"restarts"` // services that will likely restart
	RebootLikely bool     `json:"reboot_likely"`
	Hash         string   `json:"hash"` // identifies this exact plan
}

// UpgradeParams: no packages = upgrade everything upgradable; packages =
// install exactly these (kept-back) packages' new versions; Fix = repair an
// interrupted install (dpkg --configure -a, then apt-get --fix-broken install).
type UpgradeParams struct {
	Packages []string `json:"packages,omitempty"`
	Fix      bool     `json:"fix,omitempty"`
	PlanHash string   `json:"plan_hash,omitempty"` // apply only
}

func (p UpgradeParams) Validate() error {
	if len(p.Packages) > 200 {
		return fmt.Errorf("too many packages")
	}
	if p.Fix && len(p.Packages) > 0 {
		return fmt.Errorf("repair takes no packages")
	}
	for _, n := range p.Packages {
		if !pkgNameRe.MatchString(n) {
			return fmt.Errorf("invalid package name %q", n)
		}
	}
	return nil
}

type UpgradeApplyParams UpgradeParams

func (p UpgradeApplyParams) Validate() error {
	if p.PlanHash == "" {
		return fmt.Errorf("plan_hash from the preview is required")
	}
	return UpgradeParams(p).Validate()
}

var (
	remvRe = regexp.MustCompile(`^Remv (\S+)`)
	archRe = regexp.MustCompile(` \[[^\]]*\]$`) // the trailing " [amd64]" inside the parentheses
)

// parseInst reads one "Inst" line of apt-get --simulate:
//
//	Inst libgd3 [2.3.3-9ubuntu5] (2.3.3-9ubuntu5.1 Ubuntu:24.04/noble-updates [amd64])
//	Inst libgd3 [2.3.3-9ubuntu5] (2.3.3-13+deb.sury.org+1 PPA for PHP (deprecated):24.04/noble [amd64])
//	Inst libheif1 (1.17.6-1ubuntu4 Ubuntu:24.04/noble [amd64])         (new package: no [old])
//
// The origin can itself contain parentheses, so it runs to the line's last ")".
func parseInst(line string) (PlannedPkg, bool) {
	rest, ok := strings.CutPrefix(line, "Inst ")
	if !ok {
		return PlannedPkg{}, false
	}
	name, rest, _ := strings.Cut(rest, " ")
	if name == "" {
		return PlannedPkg{}, false
	}
	p := PlannedPkg{Name: name}
	if strings.HasPrefix(rest, "[") {
		old, after, ok := strings.Cut(rest[1:], "] ")
		if !ok {
			return PlannedPkg{}, false
		}
		p.From, rest = old, after
	}
	end := strings.LastIndexByte(rest, ')')
	if !strings.HasPrefix(rest, "(") || end < 1 {
		return PlannedPkg{}, false
	}
	inner := archRe.ReplaceAllString(rest[1:end], "")
	p.To, p.Origin, _ = strings.Cut(inner, " ")
	if p.To == "" {
		return PlannedPkg{}, false
	}
	p.Security = strings.Contains(p.Origin, "-security")
	return p, true
}

// parseSimulation reads `apt-get --simulate` output.
func parseSimulation(out string) UpgradePlan {
	plan := UpgradePlan{Upgrades: []PlannedPkg{}, NewPackages: []PlannedPkg{}, Removals: []string{}, KeptBack: []string{}, Phased: []string{}, Restarts: []string{}}
	var section *[]string // the list being read: kept back or phased
	var sig []string
	for _, line := range strings.Split(out, "\n") {
		if section != nil {
			if strings.HasPrefix(line, " ") {
				*section = append(*section, strings.Fields(line)...)
				continue
			}
			section = nil
		}
		switch {
		case strings.HasPrefix(line, "The following packages have been kept back"):
			section = &plan.KeptBack
		case strings.HasPrefix(line, "The following upgrades have been deferred due to phasing"):
			section = &plan.Phased
		case strings.HasPrefix(line, "Inst "):
			p, ok := parseInst(line)
			if !ok {
				continue
			}
			if p.From == "" {
				plan.NewPackages = append(plan.NewPackages, p)
			} else {
				plan.Upgrades = append(plan.Upgrades, p)
			}
			sig = append(sig, "I "+p.Name+" "+p.To)
		case strings.HasPrefix(line, "Remv "):
			if m := remvRe.FindStringSubmatch(line); m != nil {
				plan.Removals = append(plan.Removals, m[1])
				sig = append(sig, "R "+m[1])
			}
		}
	}
	restarts := map[string]bool{}
	for _, p := range append(append([]PlannedPkg{}, plan.Upgrades...), plan.NewPackages...) {
		for _, s := range serviceFor(p.Name) {
			restarts[s] = true
		}
		if rebootPackage(p.Name) {
			plan.RebootLikely = true
		}
	}
	for s := range restarts {
		plan.Restarts = append(plan.Restarts, s)
	}
	sort.Strings(plan.Restarts)
	sort.Strings(sig)
	sum := sha256.Sum256([]byte(strings.Join(sig, "\n")))
	plan.Hash = hex.EncodeToString(sum[:8])
	return plan
}

var phpPkgRe = regexp.MustCompile(`^php(\d+\.\d+)-`)

// serviceFor guesses which services an upgraded package restarts.
func serviceFor(pkg string) []string {
	switch {
	case strings.HasPrefix(pkg, "nginx"):
		return []string{"nginx"}
	case phpPkgRe.MatchString(pkg):
		return []string{"php" + phpPkgRe.FindStringSubmatch(pkg)[1] + "-fpm"}
	case strings.HasPrefix(pkg, "mysql-server"), strings.HasPrefix(pkg, "mysql-client-core"):
		return []string{"mysql"}
	case strings.HasPrefix(pkg, "mariadb-server"):
		return []string{"mariadb"}
	case pkg == "openssh-server":
		return []string{"ssh"}
	case strings.HasPrefix(pkg, "libssl"), pkg == "openssl", strings.HasPrefix(pkg, "libc6"):
		return []string{"services that use " + pkg}
	}
	return nil
}

// rebootPackage: upgrades Ubuntu marks as needing a reboot.
func rebootPackage(pkg string) bool {
	for _, p := range []string{"linux-image", "linux-modules", "linux-generic", "linux-firmware", "libc6", "dbus", "systemd"} {
		if pkg == p || strings.HasPrefix(pkg, p+"-") {
			return true
		}
	}
	return false
}

// aptBroken is the code for "a previous install was interrupted": the UI
// then offers Repair (Fix).
const aptBroken = "apt_broken"

func (u *updates) simulate(ctx context.Context, p UpgradeParams) (UpgradePlan, error) {
	args := []string{"--simulate", "-o", "Debug::NoLocking=1", "upgrade"}
	switch {
	case p.Fix:
		args = []string{"--simulate", "-o", "Debug::NoLocking=1", "--fix-broken", "install"}
	case len(p.Packages) > 0:
		args = append([]string{"--simulate", "-o", "Debug::NoLocking=1", "install", "--only-upgrade"}, p.Packages...)
	}
	// Outside the agent's sandbox, like the real install: in the sandbox's
	// own mount namespace apt thinks it's in a container and ignores
	// phasing, so the preview would list updates the install then skips.
	res, err := u.deps.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{Bin: aptGetBin, Args: args, Env: []string{"DEBIAN_FRONTEND=noninteractive"}}))
	if err != nil {
		msg := failure(res, err, 6)
		if !p.Fix && (strings.Contains(msg, "--fix-broken") || strings.Contains(msg, "Unmet dependencies") || strings.Contains(msg, "dpkg was interrupted")) {
			return UpgradePlan{}, agent.Errorf(aptBroken, "a previous install didn't finish, so apt can't plan new updates: %s", msg)
		}
		return UpgradePlan{}, agent.Errorf("apt_failed", "apt-get: %s", msg)
	}
	plan := parseSimulation(string(res.Stdout))
	if len(p.Packages) > 0 {
		for _, n := range p.Packages {
			if !slices.ContainsFunc(plan.Upgrades, func(x PlannedPkg) bool { return x.Name == n || strings.HasPrefix(x.Name, n+":") }) {
				return UpgradePlan{}, agent.Errorf(agent.CodeInvalidParams, "%s has no newer version to install", n)
			}
		}
	}
	return plan, nil
}

// updatesPlan is the preview (read-only: apt-get --simulate).
func (u *updates) updatesPlan(ctx context.Context, _ agent.Actor, p UpgradeParams) (any, error) {
	return u.simulate(ctx, p)
}

const systemdRunBin = "/usr/bin/systemd-run"

// outsideSandbox runs a package-manager command as its own transient
// systemd service instead of inside layte-agent. The agent's unit is
// hardened (ProtectKernelModules, ProtectKernelLogs, PrivateTmp, …), which
// is right for the agent but breaks dpkg: a kernel update can't write
// /lib/modules. Also, if the agent call times out, apt is not killed half
// way (a killed dpkg leaves the system half-installed); it finishes alone.
// Arguments are still passed as a list; no shell is involved.
func outsideSandbox(c system.Cmd) system.Cmd {
	args := []string{"--quiet", "--wait", "--pipe", "--collect", "--service-type=exec",
		fmt.Sprintf("--unit=layte-apt-%d", time.Now().UnixNano()), "--property=KillMode=none"}
	for _, e := range c.Env {
		args = append(args, "--setenv="+e)
	}
	args = append(append(args, "--", c.Bin), c.Args...)
	return system.Cmd{Bin: systemdRunBin, Args: args}
}

// updatesRefresh runs `apt-get update` (new package lists from the mirrors).
func (u *updates) updatesRefresh(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	res, err := u.deps.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{Bin: aptGetBin, Args: []string{"-q", "-o", "DPkg::Lock::Timeout=" + aptLockWaitSecs, "update"},
		Env: []string{"DEBIAN_FRONTEND=noninteractive"}}))
	u.mu.Lock()
	u.cached = nil
	u.mu.Unlock()
	if err != nil {
		return nil, agent.Errorf("apt_failed", "apt-get update: %s", failure(res, err, 6))
	}
	return map[string]any{"refreshed": true}, nil
}

// SiteCheck is one site's answer before and after the upgrade.
type SiteCheck struct {
	Domain string `json:"domain"`
	Before int    `json:"before"`
	After  int    `json:"after"`
}

// UpgradeResult is what updates.apply did.
type UpgradeResult struct {
	Installed      int         `json:"installed"`
	Output         string      `json:"output"` // last lines of apt's output
	Sites          []SiteCheck `json:"sites"`
	SitesChanged   int         `json:"sites_changed"`
	RebootRequired bool        `json:"reboot_required"`
	RebootPackages []string    `json:"reboot_packages,omitempty"`
	KernelPending  bool        `json:"kernel_pending"`            // a newer kernel is installed than running
	RestartPending []string    `json:"restart_pending,omitempty"` // services needrestart left for later
}

// updatesApply installs the previewed updates. It refuses if the plan
// changed since the preview or would remove anything.
func (u *updates) updatesApply(ctx context.Context, _ agent.Actor, p UpgradeApplyParams) (any, error) {
	plan, err := u.simulate(ctx, UpgradeParams(p))
	if err != nil {
		return nil, err
	}
	if plan.Hash != p.PlanHash {
		return nil, agent.Errorf(agent.CodeInvalidParams, "the pending updates changed since the preview; review them again")
	}
	if len(plan.Removals) > 0 {
		return nil, agent.Errorf(agent.CodeInvalidParams, "this would remove %s; Layte Panel never removes packages", strings.Join(plan.Removals, ", "))
	}
	if len(plan.Upgrades)+len(plan.NewPackages) == 0 && !p.Fix {
		return UpgradeResult{Sites: []SiteCheck{}}, nil
	}

	// How every site answers now, to compare afterwards.
	var sites []DiscoveredSite
	if out, err := u.deps.sitesDiscover(ctx, agent.Actor{}, agent.NoParams{}); err == nil {
		sites = out.([]DiscoveredSite)
	}
	before := map[string]int{}
	for _, s := range sites {
		if code, err := u.deps.siteStatus(ctx, s); err == nil {
			before[s.Domain] = code
		}
	}

	env := []string{
		"DEBIAN_FRONTEND=noninteractive", // never ask
		"NEEDRESTART_MODE=a",             // restart affected services automatically
		"APT_LISTCHANGES_FRONTEND=none",
		"UCF_FORCE_CONFFOLD=1", // ucf-managed configs: keep the installed one too
	}
	keepConfigs := []string{"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}
	args := append([]string{"-y", "-q", "-o", "DPkg::Lock::Timeout=" + aptLockWaitSecs}, keepConfigs...)
	var note string
	switch {
	case p.Fix:
		// Finish what dpkg had unpacked. This may fail when dependencies are
		// still missing; that's normal, apt --fix-broken install brings them
		// in and configures the rest. dpkg --audit decides at the end.
		if res, err := u.deps.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{Bin: dpkgBin, Args: []string{"--configure", "-a", "--force-confdef", "--force-confold"}, Env: env})); err != nil {
			note = "dpkg --configure -a couldn't finish everything yet (" + failure(res, err, 3) + "); apt completes it."
		}
		args = append(args, "--fix-broken", "install")
	case len(p.Packages) > 0:
		args = append(append(args, "install", "--only-upgrade"), p.Packages...)
	default:
		args = append(args, "upgrade")
	}
	res, err := u.deps.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{Bin: aptGetBin, Args: args, Env: env}))
	u.mu.Lock()
	u.cached = nil
	u.mu.Unlock()
	out := UpgradeResult{Installed: len(plan.Upgrades) + len(plan.NewPackages), Output: strings.Join(strings.Split(lastLines(res.Output(), 25), " | "), "\n"), Sites: []SiteCheck{}}
	if note != "" {
		out.Output = note + "\n" + out.Output
	}
	if err != nil {
		return nil, agent.Errorf("apt_failed", "apt-get: %s", failure(res, err, 10))
	}
	if p.Fix {
		// Only "done" if dpkg agrees nothing is left half-installed.
		if audit, _ := u.deps.Runner.Run(ctx, dpkgBin, "--audit"); strings.TrimSpace(string(audit.Stdout)) != "" {
			return nil, agent.Errorf("apt_failed", "the repair ran, but dpkg still reports problems: %s", lastLines(string(audit.Stdout), 6))
		}
	}

	// Services or a kernel that still need a restart (needrestart batch mode).
	if nr, err := u.deps.Runner.Run(ctx, needrestartBin, "-b"); err == nil || len(nr.Stdout) > 0 {
		for _, line := range strings.Split(string(nr.Stdout), "\n") {
			k, v, _ := strings.Cut(strings.TrimSpace(line), ": ")
			switch k {
			case "NEEDRESTART-KSTA":
				out.KernelPending = v == "2" || v == "3"
			case "NEEDRESTART-SVC":
				out.RestartPending = append(out.RestartPending, strings.TrimSuffix(v, ".service"))
			}
		}
	}
	if ok, _ := u.deps.FS.Exists(rebootFlagPath); ok {
		out.RebootRequired = true
		if b, err := u.deps.FS.ReadFile(rebootPkgsPath); err == nil {
			out.RebootPackages = strings.Fields(string(b))
		}
	}
	out.RebootRequired = out.RebootRequired || out.KernelPending

	for _, s := range sites {
		want, ok := before[s.Domain]
		if !ok {
			continue
		}
		got, err := u.deps.settledStatus(ctx, s, want)
		if err != nil {
			got = 0
		}
		out.Sites = append(out.Sites, SiteCheck{Domain: s.Domain, Before: want, After: got})
		if got != want {
			out.SitesChanged++
		}
	}
	return out, nil
}
