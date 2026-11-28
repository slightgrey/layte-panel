package actions

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// Disk quotas for site logins: Linux user quotas on the filesystem that
// holds /home. The kernel stops a user's writes past the hard limit
// everywhere (PHP, SFTP, cron, uploads).
//
// Turning them on (quota.enable, once, by an admin) installs the quota tools,
// adds usrquota to that filesystem's line in /etc/fstab (checked with
// findmnt --verify; the old file is restored if it fails), remounts it, builds
// the quota file and switches quotas on. Only ext4 is supported; xfs needs
// quota options at boot.

const (
	findmntBin    = "/usr/bin/findmnt"
	quotaonBin    = "/usr/sbin/quotaon"
	quotaoffBin   = "/usr/sbin/quotaoff"
	quotacheckBin = "/usr/sbin/quotacheck"
	setquotaBin   = "/usr/sbin/setquota"
	quotaBin      = "/usr/bin/quota"
	mountBinPath  = "/usr/bin/mount"
	fstabPath     = "/etc/fstab"
	quotaHome     = "/home"
)

// QuotaStatus says whether disk quotas work for site users.
type QuotaStatus struct {
	Enabled   bool   `json:"enabled"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"` // why not supported (or what's missing)
	Mount     string `json:"mount"`            // the filesystem that holds /home
	FSType    string `json:"fs_type"`
	Tools     bool   `json:"tools"` // the quota package is installed
	// Kernel: the running kernel has the quota format (quota_v2). Ubuntu's
	// cloud kernels ship it in linux-modules-extra-<version>, which turning
	// quotas on installs (KernelPackages, about 110 MB).
	Kernel         bool     `json:"kernel"`
	KernelPackages []string `json:"kernel_packages,omitempty"`
}

const (
	modprobeBin = "/usr/sbin/modprobe"
	aptCacheBin = "/usr/bin/apt-cache"
)

// kernelRelease is the running kernel's version (uname -r).
func (d Deps) kernelRelease() string {
	b, _ := d.FS.ReadFile("/proc/sys/kernel/osrelease")
	return strings.TrimSpace(string(b))
}

var kernelReleaseRe = regexp.MustCompile(`^[0-9][0-9A-Za-z.+-]{0,60}$`)

// quotaKernel reports whether the kernel can load the quota format, and if
// not, the packages that add it (only ones apt can install).
func (d Deps) quotaKernel(ctx context.Context) (bool, []string) {
	// The agent's own sandbox hides /lib/modules: ask from outside it.
	if _, err := d.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{Bin: modprobeBin, Args: []string{"--dry-run", "--quiet", "quota_v2"}})); err == nil {
		return true, nil
	}
	rel := d.kernelRelease()
	if !kernelReleaseRe.MatchString(rel) {
		return false, nil
	}
	var pkgs []string
	for _, p := range []string{"linux-modules-extra-" + rel, "linux-image-extra-virtual"} {
		if d.aptCandidate(ctx, p) {
			pkgs = append(pkgs, p)
		}
	}
	if len(pkgs) == 0 || pkgs[0] != "linux-modules-extra-"+rel {
		return false, nil // no package with this kernel's modules
	}
	return false, pkgs
}

// aptCandidate reports whether apt can install the package.
func (d Deps) aptCandidate(ctx context.Context, pkg string) bool {
	res, err := d.Runner.Run(ctx, aptCacheBin, "policy", pkg)
	return err == nil && strings.Contains(string(res.Stdout), "Candidate:") && !strings.Contains(string(res.Stdout), "Candidate: (none)")
}

// quotaMount finds the filesystem holding /home.
func (d Deps) quotaMount(ctx context.Context) (target, fstype, options string, err error) {
	res, err := d.Runner.Run(ctx, findmntBin, "--noheadings", "--output", "TARGET,FSTYPE,OPTIONS", "--target", quotaHome)
	if err != nil {
		return "", "", "", fmt.Errorf("findmnt: %s", failure(res, err, 2))
	}
	f := strings.Fields(string(res.Stdout))
	if len(f) < 3 {
		return "", "", "", fmt.Errorf("findmnt: unexpected output %q", res.Stdout)
	}
	return f[0], f[1], f[2], nil
}

func (d Deps) quotaStatus(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	return d.readQuotaStatus(ctx)
}

// quotaOn is the light part of the status: the mount and whether quota is
// on (no kernel or package checks), for setting and reading quotas.
func (d Deps) quotaOn(ctx context.Context) (QuotaStatus, error) {
	var st QuotaStatus
	target, fstype, _, err := d.quotaMount(ctx)
	if err != nil {
		return st, err
	}
	st.Mount, st.FSType = target, fstype
	if st.Tools, _ = d.FS.Exists(setquotaBin); st.Tools {
		st.Enabled = d.quotaEnabled(ctx, target)
	}
	return st, nil
}

// quotaEnabled asks quotaon from outside the agent's sandbox (its own mount
// namespace may not see the state). Its exit status is 1 when quota *is*
// on, so only the text counts.
func (d Deps) quotaEnabled(ctx context.Context, target string) bool {
	res, _ := d.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{Bin: quotaonBin, Args: []string{"--print-state", "--user", target}}))
	return strings.Contains(string(res.Stdout), " is on")
}

func (d Deps) readQuotaStatus(ctx context.Context) (QuotaStatus, error) {
	var st QuotaStatus
	target, fstype, _, err := d.quotaMount(ctx)
	if err != nil {
		return st, err
	}
	st.Mount, st.FSType = target, fstype
	st.Tools, _ = d.FS.Exists(setquotaBin)
	st.Kernel, st.KernelPackages = d.quotaKernel(ctx)
	switch {
	case fstype != "ext4":
	case !st.Kernel && len(st.KernelPackages) == 0:
		st.Reason = "the running kernel (" + d.kernelRelease() + ") has no quota support, and apt has no extra modules package for it"
	default:
		st.Supported = true
	}
	switch fstype {
	case "ext4":
	case "xfs":
		st.Reason = "xfs needs quota options at boot (rootflags=uquota); Layte Panel doesn't change boot settings"
	default:
		st.Reason = fstype + " isn't supported (ext4 only)"
	}
	if st.Tools {
		st.Enabled = d.quotaEnabled(ctx, target)
	}
	return st, nil
}

// removeUsrquota returns fstab without usrquota in the options of the line
// that mounts target (what addUsrquota added).
func removeUsrquota(fstab, target string) (string, bool) {
	lines := strings.Split(fstab, "\n")
	changed := false
	for i, l := range lines {
		f := strings.Fields(l)
		if len(f) < 4 || strings.HasPrefix(f[0], "#") || f[1] != target {
			continue
		}
		var keep []string
		for _, o := range strings.Split(f[3], ",") {
			if o == "usrquota" {
				changed = true
				continue
			}
			keep = append(keep, o)
		}
		if len(keep) == 0 {
			keep = []string{"defaults"}
		}
		lines[i] = setField(l, 3, strings.Join(keep, ","))
	}
	return strings.Join(lines, "\n"), changed
}

// quotaDisable turns user quotas off and takes usrquota out of /etc/fstab
// (so the quota service doesn't turn them back on at boot). The quota tools
// and kernel module stay; turning quotas on again is quick.
func (d Deps) quotaDisable(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	st, err := d.quotaOn(ctx)
	if err != nil {
		return nil, err
	}
	if st.Enabled {
		res, err := d.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{Bin: quotaoffBin, Args: []string{"--user", st.Mount}}))
		if err != nil {
			return nil, fmt.Errorf("quotaoff: %s", failure(res, err, 3))
		}
	}
	b, err := d.FS.ReadFile(fstabPath)
	if err != nil {
		return nil, err
	}
	if next, changed := removeUsrquota(string(b), st.Mount); changed {
		if err := d.editFstab(ctx, b, []byte(next)); err != nil {
			return nil, err
		}
	}
	d.FS.Remove(strings.TrimSuffix(st.Mount, "/") + "/aquota.user")
	return d.readQuotaStatus(ctx)
}

// addUsrquota returns fstab with usrquota added to the options of the line
// that mounts target, or an error if there is no single such line.
func addUsrquota(fstab, target string) (string, bool, error) {
	lines := strings.Split(fstab, "\n")
	found := -1
	for i, l := range lines {
		f := strings.Fields(l)
		if len(f) < 4 || strings.HasPrefix(f[0], "#") || f[1] != target {
			continue
		}
		if found >= 0 {
			return "", false, fmt.Errorf("/etc/fstab mounts %s twice", target)
		}
		found = i
	}
	if found < 0 {
		return "", false, fmt.Errorf("/etc/fstab has no line for %s", target)
	}
	f := strings.Fields(lines[found])
	opts := strings.Split(f[3], ",")
	if slices.Contains(opts, "usrquota") || slices.Contains(opts, "usrjquota=aquota.user") {
		return fstab, false, nil
	}
	lines[found] = setField(lines[found], 3, f[3]+",usrquota")
	return strings.Join(lines, "\n"), true, nil
}

// setField replaces the n-th whitespace-separated field of line, keeping
// every other character (the admin's spacing) as it was.
func setField(line string, n int, value string) string {
	i, field := 0, -1
	for i < len(line) {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		start := i
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		if start == i {
			break
		}
		if field++; field == n {
			return line[:start] + value + line[i:]
		}
	}
	return line
}

// quotaEnable turns on user quotas for the filesystem holding /home.
func (d Deps) quotaEnable(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	st, err := d.readQuotaStatus(ctx)
	if err != nil {
		return nil, err
	}
	if !st.Supported {
		return nil, agent.Errorf(agent.CodeInvalidParams, "disk quotas can't be turned on here: %s", st.Reason)
	}
	if st.Enabled {
		return st, nil
	}
	run := func(timeout time.Duration, bin string, args ...string) error {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		res, err := d.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{Bin: bin, Args: args, Env: []string{"DEBIAN_FRONTEND=noninteractive"}}))
		if err != nil {
			return fmt.Errorf("%s %s: %s", bin, strings.Join(args, " "), failure(res, err, 3))
		}
		return nil
	}
	// 1. The quota tools, and the kernel's quota format if it's missing (fixed
	// package names; nothing else is installed).
	if !st.Tools {
		if err := run(5*time.Minute, aptGetBin, "install", "-y", "--no-install-recommends", "quota"); err != nil {
			return nil, err
		}
	}
	if !st.Kernel {
		args := append([]string{"install", "-y", "--no-install-recommends"}, st.KernelPackages...)
		if err := run(10*time.Minute, aptGetBin, args...); err != nil {
			return nil, err
		}
		if err := run(time.Minute, modprobeBin, "quota_v2"); err != nil {
			return nil, err
		}
	}
	// 2. usrquota in fstab, checked before the remount relies on it.
	b, err := d.FS.ReadFile(fstabPath)
	if err != nil {
		return nil, err
	}
	next, changed, err := addUsrquota(string(b), st.Mount)
	if err != nil {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%v", err)
	}
	if changed {
		if err := d.editFstab(ctx, b, []byte(next)); err != nil {
			return nil, err
		}
	}
	// 3. Remount with it, build the quota file, switch quotas on.
	if err := run(time.Minute, mountBinPath, "-o", "remount", st.Mount); err != nil {
		return nil, fmt.Errorf("%w (fstab now has usrquota: a reboot finishes turning quotas on)", err)
	}
	if err := run(15*time.Minute, quotacheckBin, "--create-files", "--user", "--no-remount", st.Mount); err != nil {
		return nil, err
	}
	if err := run(time.Minute, quotaonBin, "--user", st.Mount); err != nil {
		return nil, err
	}
	return d.readQuotaStatus(ctx)
}

// fstabOrig is the admin's untouched /etc/fstab, kept (never deleted) the
// first time Layte changes it, so it can always be put back by hand.
const fstabOrig = fstabPath + ".layte-orig"

// editFstab replaces /etc/fstab (a file that isn't Layte's: only one mount
// option is added) and checks it with findmnt --verify; on failure the
// original goes back. old must still be the current content.
func (d Deps) editFstab(ctx context.Context, old, next []byte) error {
	if cur, err := d.FS.ReadFile(fstabPath); err != nil || string(cur) != string(old) {
		return errors.New("/etc/fstab changed while turning quotas on; try again")
	}
	if ok, _ := d.FS.Exists(fstabOrig); !ok {
		if err := d.FS.WriteFileAtomic(fstabOrig, old, 0o644); err != nil {
			return fmt.Errorf("keep a copy of /etc/fstab: %w", err)
		}
		d.FS.Chown(fstabOrig, 0, 0)
	}
	if err := d.FS.WriteFileAtomic(fstabPath, next, 0o644); err != nil {
		return fmt.Errorf("write /etc/fstab: %w", err)
	}
	// root:root like before (new files get the agent's group).
	if err := d.FS.Chown(fstabPath, 0, 0); err != nil {
		return fmt.Errorf("chown /etc/fstab: %w", err)
	}
	res, err := d.Runner.Run(ctx, findmntBin, "--verify", "--tab-file", fstabPath)
	if err != nil {
		if rerr := d.FS.WriteFileAtomic(fstabPath, old, 0o644); rerr != nil {
			return fmt.Errorf("the new /etc/fstab failed findmnt --verify (%s), and putting the old one back failed: %v; restore it from %s", failure(res, err, 3), rerr, fstabOrig)
		}
		return fmt.Errorf("the new /etc/fstab failed findmnt --verify, so it was put back: %s", failure(res, err, 3))
	}
	// systemd's fstab generator reads it at boot and on reload: with
	// usrquota, the mount pulls in quotaon.service, so quotas come back on
	// after a reboot (and stop doing so once it's removed).
	if res, err := d.Runner.Run(ctx, systemctl, "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %s", failure(res, err, 3))
	}
	return nil
}

// QuotaSetParams sets a site user's disk limit (MB; 0 = none).
type QuotaSetParams struct {
	Username string `json:"username"`
	MB       int    `json:"mb"`
}

func (p QuotaSetParams) Validate() error {
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	if p.MB < 0 || p.MB > 16<<20 {
		return errors.New("the disk limit must be 0-16777216 MB")
	}
	return nil
}

// quotaSet sets the hard limit (and a soft limit at 95% of it, so the user
// is warned first). 0 removes the limit.
func (d Deps) quotaSet(ctx context.Context, _ agent.Actor, p QuotaSetParams) (any, error) {
	if err := d.requireSiteUser(p.Username); err != nil {
		return nil, err
	}
	st, err := d.quotaOn(ctx)
	if err != nil {
		return nil, err
	}
	if !st.Enabled {
		if p.MB == 0 {
			return map[string]bool{"set": false}, nil
		}
		return nil, agent.Errorf(agent.CodeInvalidParams, "turn on disk quotas first (Users → Disk quotas)")
	}
	hardKB := int64(p.MB) * 1024
	softKB := hardKB * 95 / 100
	if res, err := d.Runner.Run(ctx, setquotaBin, "-u", p.Username, strconv.FormatInt(softKB, 10), strconv.FormatInt(hardKB, 10), "0", "0", st.Mount); err != nil {
		return nil, fmt.Errorf("setquota: %s", failure(res, err, 3))
	}
	return map[string]bool{"set": true}, nil
}

// QuotaUsage is a user's disk use and hard limit in bytes (limit 0 = none).
type QuotaUsage struct {
	Used  int64 `json:"used"`
	Limit int64 `json:"limit"`
}

// userQuota reads a user's usage and hard limit from the kernel with
// `quota` (repquota reads the quota file, which lags behind). Only when
// quotas are on.
func (d Deps) userQuota(ctx context.Context, user string) (QuotaUsage, bool) {
	st, err := d.quotaOn(ctx)
	if err != nil || !st.Enabled {
		return QuotaUsage{}, false
	}
	// -w: one line per filesystem; -p: grace as a number (fixed columns).
	res, _ := d.Runner.RunCmd(ctx, outsideSandbox(system.Cmd{Bin: quotaBin, Args: []string{"--no-wrap", "--raw-grace", "--user", "--filesystem", st.Mount, user}}))
	return parseQuota(string(res.Stdout))
}

// parseQuota reads `quota -w -p` output: a header, then
// "<fs> <blocks>[*] <soft> <hard> <grace> <files>[*] …" in 1 KB blocks.
func parseQuota(out string) (QuotaUsage, bool) {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !strings.HasPrefix(f[0], "/") {
			continue
		}
		used, err1 := strconv.ParseInt(strings.TrimSuffix(f[1], "*"), 10, 64)
		hard, err2 := strconv.ParseInt(f[3], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		return QuotaUsage{Used: used * 1024, Limit: hard * 1024}, true
	}
	// "Disk quotas for user x (uid n): none": nothing used, no limit.
	return QuotaUsage{}, strings.Contains(out, "Disk quotas for user")
}
