package actions

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"layte-panel/internal/system"
)

// Fixtures: a fake Ubuntu 24.04 host used by tests and `agent --dev`, so the
// agent never runs real commands on a developer's machine. The "all:" prefix
// includes dotfiles (the .keep files that make empty dirs exist).
//
//go:embed all:fixtures
var fixturesFS embed.FS

// DevDeps returns fake dependencies backed by the embedded fixtures.
func DevDeps() Deps {
	root, _ := fs.Sub(fixturesFS, "fixtures")
	return Deps{
		Runner: &system.FakeRunner{Handle: fixtureHandler(root)},
		FS:     system.FixtureFS{Root: root},
		Now:    time.Now,
	}
}

// fixtureCommands maps "bin args" → fixture file under commands/.
// A ".stderr.txt" suffix means the output goes to stderr (like nginx -v).
var fixtureCommands = map[string]string{
	"/usr/sbin/nginx -v":             "nginx-v.stderr.txt",
	"/usr/bin/php8.3 -v":             "php8.3-v.txt",
	"/usr/bin/php8.2 -v":             "php8.2-v.txt",
	"/usr/bin/mysql --version":       "mysql-version.txt",
	"/usr/bin/certbot --version":     "certbot-version.txt",
	"/usr/bin/apt list --upgradable": "apt-upgradable.txt",
}

func fixtureHandler(root fs.FS) func(string, []string) (system.Result, error) {
	return func(bin string, args []string) (system.Result, error) {
		key := strings.TrimSpace(bin + " " + strings.Join(args, " "))
		file, ok := fixtureCommands[key]
		if bin == systemctl && len(args) > 0 && args[0] == "show" {
			file, ok = "systemctl-show.txt", true
		}
		if !ok {
			return system.Result{}, fmt.Errorf("run %s: %w", bin, system.ErrNotFound)
		}
		b, err := fs.ReadFile(root, "commands/"+file)
		if err != nil {
			return system.Result{}, err
		}
		if strings.HasSuffix(file, ".stderr.txt") {
			return system.Result{Stderr: b}, nil
		}
		return system.Result{Stdout: b}, nil
	}
}
