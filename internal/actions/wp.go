package actions

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
	"github.com/slightgrey/layte-panel/internal/validate"
)

// WP-CLI is pinned to one release and verified by SHA-512 before use.
// To upgrade: download the new .phar and its .sha512 from the official GitHub
// release, verify, and update both constants.
const (
	wpCLIVersion = "2.12.0"
	wpCLISHA512  = "be928f6b8ca1e8dfb9d2f4b75a13aa4aee0896f8a9a0a1c45cd5d2c98605e6172e6d014dda2e27f88c98befc16c040cbb2bd1bfa121510ea5cdf5f6a30fe8832"
	wpCLIURL     = "https://github.com/wp-cli/wp-cli/releases/download/v" + wpCLIVersion + "/wp-cli-" + wpCLIVersion + ".phar"
	wpCLIPath    = "/usr/local/lib/layte/wp-cli-" + wpCLIVersion + ".phar"
)

// WPCLI provides the path of a verified wp-cli.phar.
type WPCLI interface {
	Ensure(ctx context.Context) (string, error)
}

// downloadWPCLI fetches and verifies WP-CLI once, then reuses it.
type downloadWPCLI struct {
	fs     system.FS
	client *http.Client
}

func (w downloadWPCLI) Ensure(ctx context.Context) (string, error) {
	if b, err := w.fs.ReadFile(wpCLIPath); err == nil && sha512Hex(b) == wpCLISHA512 {
		return wpCLIPath, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wpCLIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "layte-panel")
	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download WP-CLI: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download WP-CLI: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return "", fmt.Errorf("download WP-CLI: %w", err)
	}
	if got := sha512Hex(b); got != wpCLISHA512 {
		return "", fmt.Errorf("WP-CLI checksum mismatch (got %s…); refusing to use it", got[:16])
	}
	if err := w.fs.MkdirAll("/usr/local/lib/layte", 0o755); err != nil {
		return "", err
	}
	if err := w.fs.WriteFileAtomic(wpCLIPath, b, 0o644); err != nil {
		return "", err
	}
	return wpCLIPath, nil
}

func sha512Hex(b []byte) string {
	sum := sha512.Sum512(b)
	return hex.EncodeToString(sum[:])
}

type WPInstallParams struct {
	Domain        string `json:"domain"`
	Username      string `json:"username"`
	PHPVersion    string `json:"php_version"`
	URL           string `json:"url"`
	Title         string `json:"title"`
	AdminUser     string `json:"admin_user"`
	AdminEmail    string `json:"admin_email"`
	AdminPassword string `json:"admin_password"` // redacted in audit
	DBName        string `json:"db_name"`
	DBUser        string `json:"db_user"`
	DBPassword    string `json:"db_password"` // redacted in audit
}

func (p WPInstallParams) Validate() error {
	checks := []error{
		validDomain(p.Domain),
		validate.SiteUsername(p.Username),
		validate.PHPVersion(p.PHPVersion, []string{p.PHPVersion}),
		validate.Title(p.Title),
		validate.Username(p.AdminUser),
		validate.Email(p.AdminEmail),
		validate.Password(p.AdminPassword),
		validate.DBIdent(p.DBName),
		validate.DBIdent(p.DBUser),
		validate.Password(p.DBPassword),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	if p.URL != "http://"+p.Domain && p.URL != "https://"+p.Domain {
		return fmt.Errorf("url must be http(s)://%s", p.Domain)
	}
	return nil
}

// wpInstall downloads WordPress, writes wp-config.php and runs the installer,
// all as the site's user (never root). Passwords go to WP-CLI on stdin via
// --prompt, so they never appear in the process list.
func (d Deps) wpInstall(ctx context.Context, _ agent.Actor, p WPInstallParams) (any, error) {
	if err := d.requireInstalledPHP(p.PHPVersion); err != nil {
		return nil, err
	}
	cred, err := d.Users.Lookup(p.Username)
	if err != nil {
		return nil, err
	}
	phar, err := d.WPCLI.Ensure(ctx)
	if err != nil {
		return nil, err
	}
	site := templates.Site{Domain: p.Domain, User: p.Username, PHPVersion: p.PHPVersion}
	public := site.Dir() + "/public"

	// The placeholder page from site.dirs_create would shadow index.php.
	d.FS.Remove(public + "/index.html")

	wp := func(stdin string, args ...string) error {
		res, err := d.Runner.RunCmd(ctx, system.Cmd{
			Bin:   phpBin(p.PHPVersion),
			Args:  append([]string{phar, "--path=" + public}, args...),
			Stdin: []byte(stdin),
			Env:   []string{"HOME=" + site.Home(), "WP_CLI_CACHE_DIR=" + site.Dir() + "/tmp/wp-cli-cache"},
			Dir:   site.Dir(),
			As:    &cred,
		})
		if err != nil {
			return fmt.Errorf("wp %s: %s", args[0]+" "+args[1], failure(res, err, 5))
		}
		return nil
	}
	if err := wp("", "core", "download", "--locale=en_US"); err != nil {
		return nil, err
	}
	if err := wp(p.DBPassword+"\n", "config", "create", "--dbname="+p.DBName, "--dbuser="+p.DBUser, "--dbhost=localhost", "--prompt=dbpass"); err != nil {
		return nil, err
	}
	if err := wp(p.AdminPassword+"\n", "core", "install", "--url="+p.URL, "--title="+p.Title,
		"--admin_user="+p.AdminUser, "--admin_email="+p.AdminEmail, "--skip-email", "--prompt=admin_password"); err != nil {
		return nil, err
	}
	// WordPress defaults to "Plain" permalinks, which breaks /wp-json/ URLs
	// (REST API clients get the homepage). Use "Post name" like other hosts.
	if err := wp("", "rewrite", "structure", "/%postname%/"); err != nil {
		return nil, err
	}
	d.FS.Chmod(public+"/wp-config.php", 0o640)
	return map[string]string{"path": public, "wp_cli": wpCLIVersion}, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// devWPCLI is the dev/test stand-in: no download, a stub file.
type devWPCLI struct{ fs system.FS }

func (w devWPCLI) Ensure(ctx context.Context) (string, error) {
	if err := w.fs.MkdirAll("/usr/local/lib/layte", 0o755); err != nil {
		return "", err
	}
	if ok, _ := w.fs.Exists(wpCLIPath); !ok {
		w.fs.WriteFileAtomic(wpCLIPath, []byte("<?php // dev stub for wp-cli\n"), 0o644)
	}
	return wpCLIPath, nil
}

// fakeWP mirrors what WP-CLI would create, for dev mode.
func fakeWP(root system.FS, c system.Cmd) (system.Result, error) {
	var path string
	for _, a := range c.Args {
		if strings.HasPrefix(a, "--path=") {
			path = strings.TrimPrefix(a, "--path=")
		}
	}
	args := strings.Join(c.Args, " ")
	switch {
	case strings.Contains(args, " user list"):
		return system.Result{Stdout: []byte("1\n")}, nil // the first administrator
	case strings.Contains(args, " transient set "):
		return system.Result{Stdout: []byte("Success: Transient added.\n")}, nil
	case strings.Contains(args, " core download"):
		root.WriteFileAtomic(path+"/index.php", []byte("<?php // fake WordPress (dev mode)\n"), 0o644)
		root.WriteFileAtomic(path+"/wp-load.php", []byte("<?php\n"), 0o644)
	case strings.Contains(args, " config create"):
		if !bytes.HasSuffix(c.Stdin, []byte("\n")) {
			return system.Result{}, fmt.Errorf("wp config create: expected password on stdin")
		}
		root.WriteFileAtomic(path+"/wp-config.php", []byte("<?php // fake wp-config (dev mode)\n"), 0o640)
	}
	time.Sleep(300 * time.Millisecond) // feel a little like real work in the UI
	return system.Result{Stdout: []byte("Success.\n")}, nil
}

// failure describes a failed command: its last output lines, or — when it
// printed nothing (e.g. it couldn't even start) — the error itself.
func failure(res system.Result, err error, n int) string {
	if out := lastLines(res.Output(), n); out != "" {
		return out
	}
	return err.Error()
}
