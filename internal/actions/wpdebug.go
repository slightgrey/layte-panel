package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/fileops"
	"github.com/slightgrey/layte-panel/internal/system"
	"github.com/slightgrey/layte-panel/internal/templates"
)

// WP Debug: turn WP_DEBUG on or off in wp-config.php (via WP-CLI, as the
// site user) and tell the panel which log file the messages go to.
//
// Errors are never shown to visitors (WP_DEBUG_DISPLAY false), and the log
// never goes to wp-content/debug.log, which the web server would serve:
//   - Layte sites: WP_DEBUG_LOG = <site>/logs/wp-debug.log (outside public/).
//   - Adopted sites: their PHP pool is kept as it was, so PHP may not be
//     allowed to write anywhere outside the web root (open_basedir). There,
//     WP_DEBUG_LOG is off and the messages go to the pool's own PHP error log.

// WPDebugParams name a WordPress site created or adopted by Layte.
type WPDebugParams = WPLoginParams

// WPDebugSetParams turn WP Debug on or off.
type WPDebugSetParams struct {
	WPLoginParams
	Enabled bool `json:"enabled"`
}

// WPDebugState is what the WP Debug tab shows.
type WPDebugState struct {
	Enabled bool   `json:"enabled"`
	LogPath string `json:"log_path"` // where the messages go; "" if unknown
}

const wpDebugLogName = "wp-debug.log"

// wpSite is a WordPress site the agent can run WP-CLI on.
type wpSite struct {
	root     string // jail root (site folder, or the adopted web root)
	as       system.Credential
	wpPath   string // folder with wp-config.php
	config   string // wp-config.php relative to root
	logPath  string // where WP Debug writes
	setLog   bool   // whether WP_DEBUG_LOG points at logPath
	version  string // PHP version
	params   WPLoginParams
	discover DiscoveredSite
}

func (d Deps) wpSiteFor(ctx context.Context, p WPLoginParams) (wpSite, error) {
	if err := d.requireInstalledPHP(p.PHPVersion); err != nil {
		return wpSite{}, err
	}
	root, as, _, err := d.jail(ctx, FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot})
	if err != nil {
		return wpSite{}, err
	}
	s := wpSite{root: root, as: as, version: p.PHPVersion, params: p}
	if p.WebRoot == "" {
		s.wpPath, s.config = root+"/public", "public/wp-config.php"
		s.logPath = templates.Site{Domain: p.Domain, User: p.Username}.Dir() + "/logs/" + wpDebugLogName
		s.setLog = true
	} else {
		s.wpPath, s.config = root, "wp-config.php"
	}
	if s.discover, err = d.discoveredSite(ctx, p.Domain); err != nil {
		return wpSite{}, err
	}
	if !s.setLog {
		pools := d.findPools(s.discover.PHPSocket)
		if s.discover.PHPSocket != "" && len(pools) == 1 {
			s.logPath = poolErrorLog(pools[0].Content)
		}
	}
	return s, nil
}

// poolErrorLog is the pool's php_admin_value/php_value[error_log], if set.
func poolErrorLog(content string) string {
	for _, raw := range strings.Split(content, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if (k == "php_admin_value[error_log]" || k == "php_value[error_log]") && strings.HasPrefix(v, "/") && !strings.Contains(v, "$") {
			return path.Clean(v)
		}
	}
	return ""
}

// wp runs WP-CLI as the site user. Config commands only parse wp-config.php,
// they don't load plugins or themes.
func (d Deps) wpCLI(ctx context.Context, s wpSite, args ...string) (string, error) {
	phar, err := d.WPCLI.Ensure(ctx)
	if err != nil {
		return "", err
	}
	res, err := d.Runner.RunCmd(ctx, system.Cmd{
		Bin:  phpBin(s.version),
		Args: append([]string{phar, "--path=" + s.wpPath, "--skip-plugins", "--skip-themes"}, args...),
		Env:  []string{"HOME=/home/" + s.params.Username},
		Dir:  s.root,
		As:   &s.as,
	})
	if err != nil {
		return "", fmt.Errorf("wp %s: %s", strings.Join(args[:min(2, len(args))], " "), failure(res, err, 3))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// wpDebugEnabled reads WP_DEBUG from wp-config.php.
func (d Deps) wpDebugEnabled(ctx context.Context, s wpSite) (bool, error) {
	out, err := d.wpCLI(ctx, s, "config", "list", "WP_DEBUG", "--strict", "--format=json")
	if err != nil {
		if strings.Contains(err.Error(), "No matching entries") {
			return false, nil // not defined: WordPress treats it as off
		}
		return false, err
	}
	var consts []struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal([]byte(out), &consts); err != nil {
		return false, fmt.Errorf("read WP_DEBUG: %w", err)
	}
	for _, c := range consts {
		if c.Name == "WP_DEBUG" {
			switch v := c.Value.(type) {
			case bool:
				return v, nil
			case float64:
				return v != 0, nil
			case string:
				return v == "1" || strings.EqualFold(v, "true"), nil
			}
		}
	}
	return false, nil
}

func (d Deps) wpDebugGet(ctx context.Context, _ agent.Actor, p WPDebugParams) (any, error) {
	s, err := d.wpSiteFor(ctx, p)
	if err != nil {
		return nil, err
	}
	on, err := d.wpDebugEnabled(ctx, s)
	if err != nil {
		return nil, err
	}
	return WPDebugState{Enabled: on, LogPath: s.logPath}, nil
}

// wpDebugSet changes the WP_DEBUG constants, reloads the site's PHP-FPM
// (PHP caches wp-config.php) and checks the site answers as before. On any
// failure the old wp-config.php is put back.
func (d Deps) wpDebugSet(ctx context.Context, _ agent.Actor, p WPDebugSetParams) (any, error) {
	s, err := d.wpSiteFor(ctx, p.WPLoginParams)
	if err != nil {
		return nil, err
	}
	file := func(req fileops.Request) (fileops.Response, error) {
		req.Root, req.Path = s.root, s.config
		res, err := d.FileOps.Run(ctx, s.as, req)
		if err == nil && res.Error != "" {
			err = agent.Errorf("fs_error", "%s: %s", s.config, res.Error)
		}
		return res, err
	}
	orig, err := file(fileops.Request{Op: "read", Length: 256 << 10})
	if err != nil {
		return nil, err
	}
	if orig.Size > 256<<10 {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s is unexpectedly large", s.config)
	}
	before, err := d.siteStatus(ctx, s.discover)
	if err != nil {
		return nil, agent.Errorf("site_check_failed", "%s doesn't answer before the change (%v); fix that first", p.Domain, err)
	}

	set := func(name, value string, raw bool) error {
		args := []string{"config", "set", name, value, "--type=constant"}
		if raw {
			args = append(args, "--raw")
		}
		_, err := d.wpCLI(ctx, s, args...)
		return err
	}
	var steps []func() error
	if p.Enabled {
		steps = []func() error{
			func() error { return set("WP_DEBUG", "true", true) },
			func() error { return set("WP_DEBUG_DISPLAY", "false", true) },
		}
		if s.setLog {
			steps = append(steps, func() error { return set("WP_DEBUG_LOG", s.logPath, false) })
		} else {
			// Never wp-content/debug.log: the web server would serve it.
			steps = append(steps, func() error { return set("WP_DEBUG_LOG", "false", true) })
		}
	} else {
		steps = []func() error{
			func() error { return set("WP_DEBUG", "false", true) },
			func() error { return set("WP_DEBUG_LOG", "false", true) },
		}
	}

	bg := context.WithoutCancel(ctx)
	reloadPHP := func(ctx context.Context) { d.Runner.RunCmd(ctx, fpmReload(s.version)) }
	rollback := func() {
		d.FileOps.Run(bg, s.as, fileops.Request{Op: "write", Root: s.root, Path: s.config, Chunk: orig.Data, Final: true})
		reloadPHP(bg)
	}
	for _, step := range steps {
		if err := step(); err != nil {
			rollback()
			return nil, err
		}
	}
	reloadPHP(ctx)
	after, err := d.settledStatus(ctx, s.discover, before)
	if err != nil || after != before {
		rollback()
		return nil, agent.Errorf("site_check_failed", "%s answered %d before and %d after the change; wp-config.php was put back", p.Domain, before, after)
	}
	// Create the log file now, so the panel can show it right away.
	if p.Enabled && s.setLog {
		logs := "logs/" + wpDebugLogName // relative to the site folder (the jail root)
		if st, err := d.FileOps.Run(ctx, s.as, fileops.Request{Op: "stat", Root: s.root, Path: logs}); err == nil && st.Entry == nil {
			d.FileOps.Run(ctx, s.as, fileops.Request{Op: "write", Root: s.root, Path: logs, Final: true})
		}
	}
	return WPDebugState{Enabled: p.Enabled, LogPath: s.logPath}, nil
}
