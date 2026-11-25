package actions

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
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
		// Dev sites aren't served, so they always "answer" 200.
		SiteCheck: func(context.Context, string, bool) (int, error) { return 200, nil },
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
	var handle func(c system.Cmd) (system.Result, error)
	handle = func(c system.Cmd) (system.Result, error) {
		// systemd-run (outsideSandbox): answer the command it wraps.
		if c.Bin == systemdRunBin {
			if i := slices.Index(c.Args, "--"); i >= 0 && i+1 < len(c.Args) {
				inner := system.Cmd{Bin: c.Args[i+1], Args: c.Args[i+2:]}
				for _, a := range c.Args[:i] {
					if e, ok := strings.CutPrefix(a, "--setenv="); ok {
						inner.Env = append(inner.Env, e)
					}
				}
				return handle(inner)
			}
		}
		if c.Bin == dpkgBin {
			return system.Result{}, nil // dpkg --configure -a: nothing to finish in dev
		}
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
		case c.Bin == systemctl && slices.Equal(args, []string{"is-active", "sshguard"}):
			return system.Result{Stdout: []byte("active\n")}, nil
		case c.Bin == nftBin && slices.Equal(args, []string{"list", "set", "ip", "sshguard", "attackers"}):
			b, err := fs.ReadFile(cmds, "nft-sshguard-ip.txt")
			return system.Result{Stdout: b}, err
		case c.Bin == journalctlBin && slices.Contains(args, "sshguard"):
			b, err := fs.ReadFile(cmds, "journalctl-sshguard.txt")
			return system.Result{Stdout: b}, err
		case c.Bin == systemctl: // reload/restart
			return system.Result{}, nil
		case c.Bin == nginxBin && len(args) == 1 && args[0] == "-t":
			return system.Result{Stderr: []byte("nginx: configuration file /etc/nginx/nginx.conf test is successful\n")}, nil
		case c.Bin == nginxBin && len(args) == 1 && args[0] == "-T":
			return fakeNginxDump(root)
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
		case c.Bin == mysqldumpBin:
			return fakeMySQLDump(root, c)
		case c.Bin == chownBin:
			return system.Result{}, nil // the fake root doesn't track owners
		case c.Bin == aptGetBin:
			return fakeAptGet(root, cmds, args)
		case c.Bin == needrestartBin:
			return system.Result{Stdout: []byte("NEEDRESTART-VER: 3.6\nNEEDRESTART-KSTA: 1\n")}, nil
		case c.Bin == duBin:
			return fakeDu(root, c), nil
		case c.Bin == findBin && len(args) > 0:
			// Largest files: two made-up ones under the folder asked for.
			dir := strings.TrimSuffix(args[0], "/")
			return system.Result{Stdout: []byte(fmt.Sprintf("734003200\t%s/backup-old.tar.gz\n52428800\t%s/debug.log\n", dir, dir))}, nil
		case c.Bin == ufwBin:
			return fakeUFWCmd(root, c)
		case c.Bin == unattendedUpgradeBin:
			return fakeUnattendedUpgrade()
		case c.Bin == gpasswdBin, c.Bin == groupaddBin, c.Bin == "/usr/bin/id":
			return fakeGroups(root, c)
		case c.Bin == usermodBin, c.Bin == chpasswdBin:
			return system.Result{}, nil
		case c.Bin == sshdBin && len(args) > 0 && args[0] == "-t":
			return system.Result{}, nil
		case c.Bin == sshdBin && len(args) > 0 && args[0] == "-T":
			return system.Result{Stdout: []byte("port 22\npasswordauthentication yes\n")}, nil
		case c.Bin == redisCliBin:
			switch {
			case slices.Contains(args, "DBSIZE"):
				return system.Result{Stdout: []byte("42\n")}, nil
			case slices.Contains(args, "INFO"):
				return system.Result{Stdout: []byte("# Memory\r\nused_memory:1572864\r\n")}, nil
			}
			return system.Result{Stdout: []byte("OK\n")}, nil
		case c.Bin == mountpointBin:
			return system.Result{}, &system.ExitError{Bin: c.Bin, ExitCode: 1} // nothing is mounted in dev
		case strings.HasPrefix(c.Bin, "/usr/bin/php") && len(args) > 0 && strings.HasSuffix(args[0], ".phar"):
			return fakeWP(root, c)
		case c.Bin == "/usr/bin/certbot" && len(args) > 0 && args[0] != "--version":
			return fakeCertbot(root, c)
		}
		return system.Result{}, fmt.Errorf("run %s: %w", c.Bin, system.ErrNotFound)
	}
	return handle
}

// fakeDu reports made-up but stable sizes: the whole site is the sum of its
// folders plus a little, so the overview's breakdown adds up.
func fakeDu(root system.FS, c system.Cmd) system.Result {
	var paths []string
	oneLevel := false
	for i, a := range c.Args {
		if a == "--" {
			paths = c.Args[i+1:]
		}
		oneLevel = oneLevel || a == "--max-depth=1"
	}
	if oneLevel && len(paths) == 1 {
		// The dashboard's disk breakdown: each subfolder of the fake root,
		// with a made-up but stable size, then the folder's own total.
		var b strings.Builder
		var total int64
		subs, _ := root.Glob(strings.TrimSuffix(paths[0], "/") + "/*")
		for _, s := range subs {
			if fi, err := root.Lstat(s); err != nil || !fi.IsDir() {
				continue
			}
			n := int64(len(path.Base(s))*7919%900+10) * 1_048_576
			total += n
			fmt.Fprintf(&b, "%d\t%s\n", n, s)
		}
		fmt.Fprintf(&b, "%d\t%s\n", total+4096, paths[0])
		return system.Result{Stdout: []byte(b.String())}
	}
	size := func(p string) int64 {
		switch path.Base(p) {
		case "public":
			return 48_300_000
		case "logs":
			return 2_150_000
		case "tmp":
			return 640_000
		}
		return 51_200_000
	}
	var b strings.Builder
	for _, p := range paths {
		fmt.Fprintf(&b, "%d\t%s\n", size(p), p)
	}
	return system.Result{Stdout: []byte(b.String())}
}

// fakeAptGet answers apt-get in dev mode: simulations from fixtures, and a
// pretend install/update that changes nothing.
func fakeAptGet(root system.FS, cmds fs.FS, args []string) (system.Result, error) {
	switch {
	case slices.Contains(args, "--simulate") && slices.Contains(args, "install"):
		b, err := fs.ReadFile(cmds, "apt-get-sim-install-kept.txt")
		return system.Result{Stdout: b}, err
	case slices.Contains(args, "--simulate"):
		b, err := fs.ReadFile(cmds, "apt-get-sim-upgrade.txt")
		return system.Result{Stdout: b}, err
	case slices.Contains(args, "update"):
		return system.Result{Stdout: []byte("Hit:1 http://archive.ubuntu.com/ubuntu noble InRelease\nReading package lists...\n")}, nil
	}
	if slices.Contains(args, "install") && !slices.Contains(args, "--only-upgrade") {
		// Packages the object cache installs: leave the files they'd create.
		for _, a := range args {
			if a == "redis-server" {
				root.MkdirAll("/usr/bin", 0o755)
				root.WriteFileAtomic(redisServerBin, []byte("#!/bin/sh\n"), 0o755)
			}
			if v, ok := strings.CutSuffix(strings.TrimPrefix(a, "php"), "-redis"); ok && phpVersionRe.MatchString(v) {
				root.MkdirAll("/etc/php/"+v+"/mods-available", 0o755)
				root.WriteFileAtomic(redisExtIni(v), []byte("extension=redis.so\n"), 0o644)
			}
		}
	}
	time.Sleep(time.Second) // pretend to download and install
	return system.Result{Stdout: []byte("Setting up nginx (1.24.0-2ubuntu7.1) ...\n(dev mode: nothing was really installed)\n")}, nil
}

// fakeGroups mimics groupadd, gpasswd --add/--delete and id -nG with one
// file per group (its members) under /var/lib/layte-dev/groups.
func fakeGroups(root system.FS, c system.Cmd) (system.Result, error) {
	const dir = "/var/lib/layte-dev/groups"
	root.MkdirAll(dir, 0o755)
	members := func(g string) []string {
		b, _ := root.ReadFile(dir + "/" + g)
		return strings.Fields(string(b))
	}
	save := func(g string, m []string) error {
		return root.WriteFileAtomic(dir+"/"+g, []byte(strings.Join(m, "\n")), 0o644)
	}
	a := c.Args
	switch {
	case c.Bin == groupaddBin && len(a) > 0:
		g := a[len(a)-1]
		if ok, _ := root.Exists(dir + "/" + g); !ok {
			return system.Result{}, save(g, nil)
		}
		return system.Result{}, nil
	case c.Bin == gpasswdBin && len(a) == 3 && a[0] == "--add":
		m := members(a[2])
		if !slices.Contains(m, a[1]) {
			m = append(m, a[1])
		}
		return system.Result{}, save(a[2], m)
	case c.Bin == gpasswdBin && len(a) == 3 && a[0] == "--delete":
		m := members(a[2])
		if !slices.Contains(m, a[1]) {
			return system.Result{Stderr: []byte("gpasswd: user is not a member\n")}, &system.ExitError{Bin: c.Bin, ExitCode: 3}
		}
		return system.Result{}, save(a[2], slices.DeleteFunc(m, func(u string) bool { return u == a[1] }))
	case c.Bin == "/usr/bin/id" && len(a) == 2 && a[0] == "-nG":
		groups := []string{a[1]}
		files, _ := root.Glob(dir + "/*")
		for _, f := range files {
			if slices.Contains(members(path.Base(f)), a[1]) {
				groups = append(groups, path.Base(f))
			}
		}
		return system.Result{Stdout: []byte(strings.Join(groups, " ") + "\n")}, nil
	}
	return system.Result{}, nil // other gpasswd uses (e.g. nginx into a site group)
}
