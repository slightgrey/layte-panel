package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/validate"
)

const (
	systemdRunBin = "/usr/bin/systemd-run"
	setprivBin    = "/usr/bin/setpriv"
	shBin         = "/bin/sh"
)

// runCronExec runs one cron job of a site login's site inside the login's
// resource limits. cron starts it as root with only the site and the job
// number (`layte cron-exec <domain> <n>`); the command itself comes from the
// agent's root-only file, and runs as the site's user:
//
//	systemd-run --scope --slice=layte-acct-<user>.slice -- setpriv --reuid=<user> … /bin/sh -c '…'
//
// The shell (and its >> redirect into the site's cron.log) runs as the user,
// so nothing the user controls is ever handled as root.
func runCronExec(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: layte cron-exec <domain> <job number>")
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("cron-exec runs as root on Linux (from /etc/cron.d)")
	}
	domain := args[0]
	if d, err := validate.Domain(domain); err != nil || d != domain {
		return fmt.Errorf("invalid domain %q", domain)
	}
	n, err := strconv.Atoi(args[1])
	if err != nil || n < 0 {
		return fmt.Errorf("invalid job number %q", args[1])
	}

	path := actions.CronJobsPath(domain)
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	// Only the agent (root) may have written it.
	if owner, ok := system.Owner(fi); !ok || owner.UID != 0 || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s must be a root-owned file not writable by others", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var jobs actions.CronJobs
	if err := json.Unmarshal(b, &jobs); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if jobs.Domain != domain || n >= len(jobs.Commands) {
		return fmt.Errorf("no job %d for %s", n, domain)
	}
	if err := validate.SiteUsername(jobs.Username); err != nil {
		return err
	}
	u, err := user.Lookup(jobs.Username)
	if err != nil {
		return err
	}
	uid, _ := strconv.ParseUint(u.Uid, 10, 32)
	if jobs.Slice != actions.AccountSlice(uint32(uid)) || strings.ContainsAny(jobs.Dir+jobs.LogFile, "'\n") {
		return fmt.Errorf("%s doesn't look like Layte's", path)
	}
	// What cron would run for a site without a login, as the user.
	script := fmt.Sprintf("cd '%s' && ( %s ) >> '%s' 2>&1", jobs.Dir, jobs.Commands[n], jobs.LogFile)
	cmd := exec.Command(systemdRunBin, "--scope", "--quiet", "--collect", "--slice="+jobs.Slice, "--",
		setprivBin, "--reuid="+jobs.Username, "--regid="+jobs.Username, "--init-groups", "--reset-env",
		shBin, "-c", script)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, os.Stdout, os.Stderr
	cmd.Dir = "/"
	return cmd.Run()
}
