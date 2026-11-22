package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// CronPath is the site's file in /etc/cron.d. cron ignores files whose names
// contain dots, so the domain's dots become underscores.
func CronPath(domain string) string {
	return "/etc/cron.d/layte-" + strings.ReplaceAll(domain, ".", "_")
}

// WPCronCommand is the suggested cron command to run WordPress's scheduled
// events with WP-CLI (as the site user, with the site's PHP version).
func WPCronCommand(s templates.Site) string {
	return WPCronCommandAt(s.PHPVersion, s.Dir()+"/public")
}

// WPCronCommandAt is WPCronCommand for a WordPress in webRoot (adopted sites).
func WPCronCommandAt(phpVersion, webRoot string) string {
	return fmt.Sprintf("%s %s --path=%s cron event run --due-now --quiet", phpBin(phpVersion), wpCLIPath, webRoot)
}

type CronEntry struct {
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

type CronWriteParams struct {
	Domain   string      `json:"domain"`
	Username string      `json:"username"`
	WebRoot  string      `json:"web_root,omitempty"` // adopted sites: jobs run from here
	Jobs     []CronEntry `json:"jobs"`
}

func (p CronWriteParams) Validate() error {
	if err := validDomain(p.Domain); err != nil {
		return err
	}
	if err := validate.SiteUsername(p.Username); err != nil {
		return err
	}
	if p.WebRoot != "" {
		if err := (FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot, Op: "stat", Path: "."}).validate(readOps); err != nil {
			return err
		}
	}
	if len(p.Jobs) > 100 {
		return errors.New("at most 100 cron jobs per site")
	}
	for _, j := range p.Jobs {
		if err := validate.CronSchedule(j.Schedule); err != nil {
			return err
		}
		if err := validate.CronCommand(j.Command); err != nil {
			return err
		}
	}
	return nil
}

// cronWrite installs the site's cron jobs as /etc/cron.d/layte-<site>. Every
// line runs as the site's own user (never root), with output appended to the
// site's logs/cron.log. An empty list removes the file.
func (d Deps) cronWrite(ctx context.Context, _ agent.Actor, p CronWriteParams) (any, error) {
	path := CronPath(p.Domain)
	if len(p.Jobs) == 0 {
		if err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{Path: path}); err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "jobs": 0}, nil
	}
	if _, err := d.Users.Lookup(p.Username); err != nil {
		return nil, err
	}
	// Layte sites run from their folder and log to its logs/; adopted sites run
	// from their web root and log to /home/<user>/logs (outside the web root).
	dir := templates.Site{Domain: p.Domain, User: p.Username}.Dir()
	logDir := dir + "/logs"
	if p.WebRoot != "" {
		if err := d.verifyAdopted(ctx, p.Domain, p.Username, p.WebRoot); err != nil {
			return nil, err
		}
		dir, logDir = p.WebRoot, "/home/"+p.Username+"/logs"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — do not edit by hand (site: %s)\n", system.Marker, p.Domain)
	b.WriteString("SHELL=/bin/sh\nPATH=/usr/local/bin:/usr/bin:/bin\nMAILTO=\"\"\n")
	for _, j := range p.Jobs {
		// cron treats an unescaped % as a newline, so escape them.
		cmd := strings.ReplaceAll(j.Command, "%", `\%`)
		fmt.Fprintf(&b, "%s %s cd %s && ( %s ) >> %s/cron.log 2>&1\n",
			strings.Join(strings.Fields(j.Schedule), " "), p.Username, dir, cmd, logDir)
	}
	if err := system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{Path: path, Content: []byte(b.String()), Perm: 0o644}); err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "jobs": len(p.Jobs)}, nil
}
