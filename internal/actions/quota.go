package actions

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
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
	quotacheckBin = "/usr/sbin/quotacheck"
	setquotaBin   = "/usr/sbin/setquota"
	repquotaBin   = "/usr/sbin/repquota"
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

func (d Deps) readQuotaStatus(ctx context.Context) (QuotaStatus, error) {
	var st QuotaStatus
	target, fstype, _, err := d.quotaMount(ctx)
	if err != nil {
		return st, err
	}
	st.Mount, st.FSType = target, fstype
	st.Tools, _ = d.FS.Exists(setquotaBin)
	switch fstype {
	case "ext4":
		st.Supported = true
	case "xfs":
		st.Reason = "xfs needs quota options at boot (rootflags=uquota); Layte Panel doesn't change boot settings"
	default:
		st.Reason = fstype + " isn't supported (ext4 only)"
	}
	if st.Tools {
		res, err := d.Runner.Run(ctx, quotaonBin, "--print-state", "--user", target)
		st.Enabled = err == nil && strings.Contains(string(res.Stdout), " is on")
	}
	return st, nil
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
	f[3] = f[3] + ",usrquota"
	lines[found] = strings.Join(f, "\t")
	return strings.Join(lines, "\n"), true, nil
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
	// 1. The quota tools (a fixed package; nothing else is installed).
	if !st.Tools {
		if err := run(5*time.Minute, aptGetBin, "install", "-y", "--no-install-recommends", "quota"); err != nil {
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
	}
	if err := d.FS.WriteFileAtomic(fstabPath, next, 0o644); err != nil {
		return fmt.Errorf("write /etc/fstab: %w", err)
	}
	res, err := d.Runner.Run(ctx, findmntBin, "--verify", "--tab-file", fstabPath)
	if err != nil {
		if rerr := d.FS.WriteFileAtomic(fstabPath, old, 0o644); rerr != nil {
			return fmt.Errorf("the new /etc/fstab failed findmnt --verify (%s), and putting the old one back failed: %v; restore it from %s", failure(res, err, 3), rerr, fstabOrig)
		}
		return fmt.Errorf("the new /etc/fstab failed findmnt --verify, so it was put back: %s", failure(res, err, 3))
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
	st, err := d.readQuotaStatus(ctx)
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

// userQuota reads a user's usage from repquota (only when quotas are on).
func (d Deps) userQuota(ctx context.Context, user string) (QuotaUsage, bool) {
	st, err := d.readQuotaStatus(ctx)
	if err != nil || !st.Enabled {
		return QuotaUsage{}, false
	}
	res, err := d.Runner.Run(ctx, repquotaBin, "--user", "--output=csv", st.Mount)
	if err != nil {
		return QuotaUsage{}, false
	}
	rows, err := csv.NewReader(strings.NewReader(string(res.Stdout))).ReadAll()
	if err != nil || len(rows) < 2 {
		return QuotaUsage{}, false
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}
	ui, okU := col["User"]
	bu, okB := col["BlockUsed"]
	bh, okH := col["BlockHardLimit"]
	if !okU || !okB || !okH {
		return QuotaUsage{}, false
	}
	for _, r := range rows[1:] {
		if len(r) > bh && len(r) > bu && r[ui] == user {
			used, _ := strconv.ParseInt(r[bu], 10, 64)
			hard, _ := strconv.ParseInt(r[bh], 10, 64)
			return QuotaUsage{Used: used * 1024, Limit: hard * 1024}, true
		}
	}
	return QuotaUsage{}, true // no row yet: nothing used
}
