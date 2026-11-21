package actions

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/system"
)

// Fixtures: a fake Ubuntu 24.04 host used by tests and `agent --dev`, so the
// agent never runs real commands on a developer's machine. The "all:" prefix
// includes dotfiles (the .keep files that make empty dirs exist).
//
//go:embed all:fixtures
var fixturesFS embed.FS

// DevDeps returns fake dependencies rooted at dir (e.g. ./.dev/fakeroot).
// On first use dir is seeded with the embedded fake host, after which it is a
// normal directory: sites created in dev mode are written there and show up
// on the next scan. Commands are answered by a fake runner.
func DevDeps(dir string) (Deps, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return Deps{}, err
	}
	if err := seedFakeRoot(dir); err != nil {
		return Deps{}, err
	}
	root, err := system.OpenRootFS(dir)
	if err != nil {
		return Deps{}, err
	}
	root.NoChown = true
	// Windows needs a special privilege for real symlinks.
	root.EmulateSymlinks = runtime.GOOS == "windows"
	if err := seedFakeCerts(root); err != nil {
		return Deps{}, err
	}

	cmds, _ := fs.Sub(fixturesFS, "fixtures/commands")
	return Deps{
		Runner:  &system.FakeRunner{Handle: devHandler(cmds, root)},
		FS:      root,
		Users:   system.DirUsers{FS: root},
		WPCLI:   devWPCLI{fs: root},
		FileOps: localFileOps{base: absDir},
		Now:     time.Now,
	}, nil
}

// seedFakeRoot copies embedded fixtures (except commands/) that are missing
// from dir. Existing files are left alone, so dev changes survive restarts
// and new fixtures appear after an upgrade.
func seedFakeRoot(dir string) error {
	err := fs.WalkDir(fixturesFS, "fixtures", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath := strings.TrimPrefix(strings.TrimPrefix(p, "fixtures"), "/")
		if relPath == "commands" && d.IsDir() {
			return fs.SkipDir
		}
		dst := filepath.Join(dir, filepath.FromSlash(relPath))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if _, err := os.Lstat(dst); err == nil {
			return nil // already there
		}
		b, err := fixturesFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		return fmt.Errorf("seed fake root %s: %w", dir, err)
	}
	return nil
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

// devHandler answers read commands from fixtures and pretends mutating
// commands (config tests, reloads, useradd, mysql, wp-cli) succeed. Commands
// that create or delete users mirror the change in the fake root, so DirUsers
// sees them.
func devHandler(cmds fs.FS, root system.FS) func(system.Cmd) (system.Result, error) {
	return func(c system.Cmd) (system.Result, error) {
		if file, ok := fixtureCommands[c.String()]; ok {
			b, err := fs.ReadFile(cmds, file)
			if err != nil {
				return system.Result{}, err
			}
			if strings.HasSuffix(file, ".stderr.txt") {
				return system.Result{Stderr: b}, nil
			}
			return system.Result{Stdout: b}, nil
		}
		args := c.Args
		switch {
		case c.Bin == systemctl && len(args) > 0 && args[0] == "show":
			b, err := fs.ReadFile(cmds, "systemctl-show.txt")
			return system.Result{Stdout: b}, err
		case c.Bin == systemctl: // reload/restart
			return system.Result{}, nil
		case c.Bin == nginxBin && len(args) == 1 && args[0] == "-t":
			return system.Result{Stderr: []byte("nginx: configuration file /etc/nginx/nginx.conf test is successful\n")}, nil
		case strings.HasPrefix(path.Base(c.Bin), "php-fpm") && len(args) == 1 && args[0] == "-t":
			return system.Result{Stderr: []byte("NOTICE: configuration file test is successful\n")}, nil
		case c.Bin == useraddBin && len(args) > 0:
			u := args[len(args)-1]
			return system.Result{}, root.MkdirAll("/home/"+u, 0o750)
		case c.Bin == userdelBin && len(args) > 0:
			u := args[len(args)-1]
			return system.Result{}, root.RemoveAll("/home/" + u)
		case c.Bin == mysqlBin:
			return fakeMySQL(root, c)
		case c.Bin == usermodBin, c.Bin == gpasswdBin:
			return system.Result{}, nil
		case strings.HasPrefix(c.Bin, "/usr/bin/php") && len(args) > 0 && strings.HasSuffix(args[0], ".phar"):
			return fakeWP(root, c)
		case c.Bin == "/usr/bin/certbot" && len(args) > 0 && args[0] != "--version":
			return fakeCertbot(root, c)
		}
		return system.Result{}, fmt.Errorf("run %s: %w", c.Bin, system.ErrNotFound)
	}
}
