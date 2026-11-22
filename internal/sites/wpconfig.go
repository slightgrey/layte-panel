package sites

import (
	"context"
	"regexp"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/fileops"
	"github.com/slightgrey/layte-panel/internal/store"
)

// wpDefineRe matches define( 'DB_NAME', 'value' ) in wp-config.php, with
// either quote style and any spacing.
var wpDefineRe = regexp.MustCompile(`define\(\s*['"](DB_NAME|DB_USER)['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)

// wpDBConfig is the database a WordPress site's wp-config.php names.
type wpDBConfig struct {
	Name, User string
}

// parseWPConfig reads DB_NAME and DB_USER (never the password).
func parseWPConfig(src []byte) wpDBConfig {
	var c wpDBConfig
	for _, m := range wpDefineRe.FindAllSubmatch(src, -1) {
		switch string(m[1]) {
		case "DB_NAME":
			c.Name = string(m[2])
		case "DB_USER":
			c.User = string(m[2])
		}
	}
	return c
}

// readWPConfig reads wp-config.php from an adopted site's web root, as the
// site user through the agent's checked file access. ok is false if there
// is none (WordPress also allows it one level up, outside the web root).
func (s *Service) readWPConfig(ctx context.Context, actor agent.Actor, site store.Site) (cfg wpDBConfig, ok bool, err error) {
	p := actions.FSReadParams{Domain: site.Domain, Username: site.SystemUser, WebRoot: site.RootPath, Op: "read", Path: "wp-config.php", Length: 256 << 10}
	var res fileops.Response
	if err := s.Agent.Call(ctx, "fs.read", actor, p, &res); err != nil {
		if agent.IsCode(err, "fs_error") || agent.IsCode(err, agent.CodeNotFound) {
			return cfg, false, nil
		}
		return cfg, false, err
	}
	return parseWPConfig(res.Data), true, nil
}
