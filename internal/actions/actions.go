// Package actions implements the agent's allowlisted actions, one file per domain.
package actions

import (
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
	Now    func() time.Time
}

// HostDeps returns dependencies that operate on the real machine.
func HostDeps() (Deps, error) {
	root, err := system.OpenRootFS("/")
	if err != nil {
		return Deps{}, err
	}
	return Deps{Runner: system.ExecRunner{}, FS: root, Users: system.HostUsers{}, Now: time.Now}, nil
}

// Register adds every action to the registry.
func Register(reg *agent.Registry, d Deps) {
	if d.Now == nil {
		d.Now = time.Now
	}
	u := &updates{deps: d}
	reg.Register(
		agent.Def("system.info", true, 5*time.Second, d.systemInfo),
		agent.Def("services.status", true, 10*time.Second, d.servicesStatus),
		agent.Def("versions.get", true, 20*time.Second, d.versions),
		agent.Def("updates.pending", true, 60*time.Second, u.pending),

		agent.Def("sites.discover", true, 20*time.Second, d.sitesDiscover),
		agent.Def("php.versions", true, 5*time.Second, d.phpVersionsAction),
		agent.Def("nginx.vhost_read", true, 5*time.Second, d.vhostRead),
	)
}
