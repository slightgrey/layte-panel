// Package actions implements the agent's allowlisted actions, one file per domain.
package actions

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

// Deps are the host interfaces actions use. Real ones in production,
// fakes in tests and `agent --dev`.
type Deps struct {
	Runner  system.Runner
	FS      system.FS
	Users   system.Users
	WPCLI   WPCLI
	FileOps FileOps
	Now     func() time.Time
	// SiteCheck returns a site's HTTP status (nil = real request over
	// loopback). Used by adoption to verify the site still works.
	SiteCheck func(ctx context.Context, domain string, https bool) (int, error)
}

// HostDeps returns dependencies that operate on the real machine.
func HostDeps() (Deps, error) {
	root, err := system.OpenRootFS("/")
	if err != nil {
		return Deps{}, err
	}
	self, err := os.Executable()
	if err != nil {
		return Deps{}, fmt.Errorf("find own binary for the file helper: %w", err)
	}
	return Deps{
		Runner:  system.ExecRunner{},
		FS:      root,
		Users:   system.HostUsers{},
		WPCLI:   downloadWPCLI{fs: root, client: &http.Client{Timeout: 2 * time.Minute}},
		FileOps: helperFileOps{bin: self, runner: system.ExecRunner{}},
		Now:     time.Now,
	}, nil
}

// Register adds every action to the registry. The second argument of
// agent.Def says whether the action is read-only; everything else is
// written to the audit log.
func Register(reg *agent.Registry, d Deps) {
	if d.Now == nil {
		d.Now = time.Now
	}
	u := &updates{deps: d}
	reg.Register(
		// Read-only.
		agent.Def("system.info", true, 5*time.Second, d.systemInfo),
		agent.Def("services.status", true, 10*time.Second, d.servicesStatus),
		agent.Def("versions.get", true, 20*time.Second, d.versions),
		agent.Def("updates.pending", true, 60*time.Second, u.pending),
		agent.Def("updates.plan", true, 2*time.Minute, u.updatesPlan),
		agent.Def("sites.discover", true, 20*time.Second, d.sitesDiscover),
		agent.Def("php.versions", true, 5*time.Second, d.phpVersionsAction),
		agent.Def("nginx.vhost_read", true, 5*time.Second, d.vhostRead),
		agent.Def("ssl.list", true, 10*time.Second, d.sslList),
		agent.Def("db.list", true, 15*time.Second, d.dbList),
		agent.Def("logs.sources", true, 10*time.Second, d.logsSources),
		agent.Def("logs.read", true, 10*time.Second, d.logsRead).WithQuiet(),
		agent.Def("fs.read", true, 30*time.Second, d.fsRead),
		agent.Def("security.overview", true, 15*time.Second, d.securityOverview),
		agent.Def("site.disk_usage", true, 60*time.Second, d.siteDiskUsage),
		agent.Def("site.adopt_preview", true, 30*time.Second, d.adoptPreview),
		agent.Def("system.processes", true, 15*time.Second, d.systemProcesses),
		agent.Def("system.disk_breakdown", true, 3*time.Minute, d.diskBreakdown),

		// Mutating (audited).
		agent.Def("site.user_create", false, 30*time.Second, d.siteUserCreate),
		agent.Def("site.user_remove", false, 60*time.Second, d.siteUserRemove),
		agent.Def("site.dirs_create", false, 15*time.Second, d.siteDirsCreate),
		agent.Def("php.pool_write", false, 30*time.Second, d.phpPoolWrite),
		agent.Def("php.pool_remove", false, 30*time.Second, d.phpPoolRemove),
		agent.Def("nginx.vhost_write", false, 30*time.Second, d.nginxVhostWrite),
		agent.Def("nginx.vhost_remove", false, 30*time.Second, d.nginxVhostRemove),
		agent.Def("db.create", false, 30*time.Second, d.dbCreate),
		agent.Def("db.drop", false, 30*time.Second, d.dbDrop),
		agent.Def("db.password_reset", false, 30*time.Second, d.dbPasswordReset),
		agent.Def("db.wp_password", false, 2*time.Minute, d.wpDBPassword),
		agent.Def("db.export", false, 30*time.Minute, d.dbExport),
		agent.Def("db.import", false, 30*time.Minute, d.dbImport),
		agent.Def("wp.install", false, 5*time.Minute, d.wpInstall),
		agent.Def("wp.set_url", false, time.Minute, d.wpSetURL),
		agent.Def("wp.login_token", false, time.Minute, d.wpLoginToken),
		agent.Def("ssl.issue", false, 3*time.Minute, d.sslIssue),
		agent.Def("ssl.revoke", false, time.Minute, d.sslRevoke),
		agent.Def("ssl.renew", false, 3*time.Minute, d.sslRenew),
		agent.Def("cron.write", false, 15*time.Second, d.cronWrite),
		agent.Def("fs.write", false, 30*time.Second, d.fsWrite).WithAuditSkip(auditFirstChunk),
		agent.Def("ufw.allow", false, 20*time.Second, d.ufwAllow),
		agent.Def("ufw.delete", false, 20*time.Second, d.ufwDelete),
		agent.Def("ufw.set_enabled", false, 30*time.Second, d.ufwSetEnabled),
		agent.Def("cloudflare.set_realip", false, 30*time.Second, d.cloudflareSet),
		agent.Def("updates.install", false, 30*time.Minute, u.install),
		agent.Def("updates.refresh", false, 5*time.Minute, u.updatesRefresh),
		agent.Def("updates.apply", false, 45*time.Minute, u.updatesApply),
		agent.Def("site.adopt", false, 5*time.Minute, d.siteAdopt),
	)
}
