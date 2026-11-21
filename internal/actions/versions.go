package actions

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

// Version is one row of versions.get.
type Version struct {
	Name      string `json:"name"` // "nginx", "php8.3", "mysql", "mariadb", "certbot"
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

var (
	reNginx   = regexp.MustCompile(`nginx/([0-9][0-9.]*)`)
	rePHP     = regexp.MustCompile(`^PHP ([0-9][0-9.]*)`)
	reMariaDB = regexp.MustCompile(`([0-9]+\.[0-9]+\.[0-9]+)-MariaDB`)
	reMySQL   = regexp.MustCompile(`Ver ([0-9]+\.[0-9]+\.[0-9]+)`)
	reCertbot = regexp.MustCompile(`certbot ([0-9][0-9.]*)`)
	rePHPBin  = regexp.MustCompile(`^/usr/bin/php([0-9]+\.[0-9]+)$`)
)

func (d Deps) versions(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	out := []Version{}

	out = append(out, d.probe(ctx, "nginx", "/usr/sbin/nginx", []string{"-v"}, func(s string) (string, string) {
		return "nginx", firstMatch(reNginx, s)
	}))

	bins, _ := d.FS.Glob("/usr/bin/php*.*")
	for _, bin := range bins {
		m := rePHPBin.FindStringSubmatch(bin)
		if m == nil {
			continue
		}
		name := "php" + m[1]
		out = append(out, d.probe(ctx, name, bin, []string{"-v"}, func(s string) (string, string) {
			return name, firstMatch(rePHP, s)
		}))
	}

	out = append(out, d.probe(ctx, "mysql", "/usr/bin/mysql", []string{"--version"}, parseMySQLVersion))

	certbot := d.probe(ctx, "certbot", "/usr/bin/certbot", []string{"--version"}, func(s string) (string, string) {
		return "certbot", firstMatch(reCertbot, s)
	})
	if !certbot.Installed { // snap installs live elsewhere
		certbot = d.probe(ctx, "certbot", "/snap/bin/certbot", []string{"--version"}, func(s string) (string, string) {
			return "certbot", firstMatch(reCertbot, s)
		})
	}
	out = append(out, certbot)
	return out, nil
}

// probe runs bin and parses its version. "Not installed" is a normal result,
// not an error, so one missing package doesn't fail the whole action.
func (d Deps) probe(ctx context.Context, name, bin string, args []string, parse func(string) (string, string)) Version {
	res, err := d.Runner.Run(ctx, bin, args...)
	if errors.Is(err, system.ErrNotFound) {
		return Version{Name: name}
	}
	if err != nil {
		return Version{Name: name, Installed: true, Error: err.Error()}
	}
	// Many tools (nginx -v) print their version to stderr.
	n, v := parse(string(res.Stdout) + "\n" + string(res.Stderr))
	return Version{Name: n, Installed: true, Version: v}
}

// parseMySQLVersion handles both MySQL and MariaDB clients:
//
//	mysql  Ver 8.0.39-0ubuntu0.24.04.2 for Linux on x86_64 ((Ubuntu))
//	mysql  Ver 15.1 Distrib 10.11.8-MariaDB, for debian-linux-gnu (x86_64)
//	/usr/bin/mysql from 11.4.2-MariaDB, client 15.2 for debian-linux-gnu (x86_64)
func parseMySQLVersion(s string) (string, string) {
	if strings.Contains(s, "MariaDB") {
		return "mariadb", firstMatch(reMariaDB, s)
	}
	return "mysql", firstMatch(reMySQL, s)
}

func firstMatch(re *regexp.Regexp, s string) string {
	for _, line := range strings.Split(s, "\n") {
		if m := re.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return m[1]
		}
	}
	return ""
}
