package actions

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"layte-panel/internal/agent"
)

const systemctl = "/usr/bin/systemctl"

// coreServices are always checked; PHP-FPM units are discovered from /etc/php.
var coreServices = []string{"nginx", "mysql", "mariadb", "cron", "ssh", "ufw"}

var phpVersionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// Service is one row of services.status.
type Service struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	Active      string `json:"active"`  // active, inactive, failed, ...
	Sub         string `json:"sub"`     // running, dead, exited, ...
	Enabled     string `json:"enabled"` // enabled, disabled, static, ...
}

func (d Deps) servicesStatus(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	units := append([]string{}, coreServices...)
	php, err := d.phpVersions()
	if err != nil {
		return nil, err
	}
	for _, v := range php {
		units = append(units, "php"+v+"-fpm")
	}

	args := []string{"show", "--no-pager", "--property=Id,Description,LoadState,ActiveState,SubState,UnitFileState"}
	for _, u := range units {
		args = append(args, u+".service")
	}
	res, err := d.Runner.Run(ctx, systemctl, args...)
	if err != nil {
		return nil, fmt.Errorf("systemctl show: %w", err)
	}
	return parseSystemctlShow(string(res.Stdout)), nil
}

// phpVersions lists installed PHP-FPM versions ("8.3") from /etc/php/*/fpm.
func (d Deps) phpVersions() ([]string, error) {
	dirs, err := d.FS.Glob("/etc/php/*/fpm")
	if err != nil {
		return nil, fmt.Errorf("glob php versions: %w", err)
	}
	var out []string
	for _, dir := range dirs {
		v := path.Base(path.Dir(dir))
		if phpVersionRe.MatchString(v) {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out, nil
}

// parseSystemctlShow parses blank-line-separated blocks of KEY=value lines.
func parseSystemctlShow(out string) []Service {
	services := []Service{}
	seen := map[string]bool{}
	for _, block := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n\n") {
		kv := parseKeyValues(block)
		id := kv["Id"]
		// On MariaDB hosts mysql.service is an alias, so the same Id can appear twice.
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		services = append(services, Service{
			Name:        strings.TrimSuffix(id, ".service"),
			Description: kv["Description"],
			Installed:   kv["LoadState"] != "not-found",
			Active:      kv["ActiveState"],
			Sub:         kv["SubState"],
			Enabled:     kv["UnitFileState"],
		})
	}
	return services
}
