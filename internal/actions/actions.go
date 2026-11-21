// Package actions implements the agent's allowlisted actions, one file per domain.
package actions

import (
	"net/http"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

// Deps are the host interfaces actions use. Real ones in production,
// fakes in tests and `agent --dev`.
type Deps struct {
	Runner system.Runner
	FS     system.FS
	Users  system.Users
	WPCLI  WPCLI
	Now    func() time.Time
}

// HostDeps returns dependencies that operate on the real machine.
func HostDeps() (Deps, error) {
	root, err := system.OpenRootFS("/")
	if err != nil {
		return Deps{}, err
	}
	return Deps{
		Runner: system.ExecRunner{},
		FS:     root,
		Users:  system.HostUsers{},
		WPCLI:  downloadWPCLI{fs: root, client: &http.Client{Timeout: 2 * time.Minute}},
		Now:    time.Now,
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
		agent.Def("sites.discover", true, 20*time.Second, d.sitesDiscover),
		agent.Def("php.versions", true, 5*time.Second, d.phpVersionsAction),
		agent.Def("nginx.vhost_read", true, 5*time.Second, d.vhostRead),
		agent.Def("ssl.list", true, 10*time.Second, d.sslList),

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
		agent.Def("wp.install", false, 5*time.Minute, d.wpInstall),
		agent.Def("wp.set_url", false, time.Minute, d.wpSetURL),
		agent.Def("ssl.issue", false, 3*time.Minute, d.sslIssue),
		agent.Def("ssl.revoke", false, time.Minute, d.sslRevoke),
	)
}
