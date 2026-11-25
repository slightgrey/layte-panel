package actions

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/nginx"
	"github.com/slightgrey/layte-panel/internal/system"
)

// allowedLogs are the only files logs.read will open. Paths are checked both
// as given and after following symlinks, so a link can't smuggle in
// /etc/shadow.
var allowedLogs = []*regexp.Regexp{
	regexp.MustCompile(`^/var/log/nginx/[A-Za-z0-9._-]+\.log$`),
	regexp.MustCompile(`^/var/log/php[0-9]+\.[0-9]+-fpm\.log$`),
	regexp.MustCompile(`^/home/[a-z][a-z0-9_-]{2,31}/[a-z0-9.-]+/logs/[A-Za-z0-9._-]+\.log$`),
	// Adopted sites: their logs/ folder is in the user's home.
	regexp.MustCompile(`^/home/[a-z][a-z0-9_-]{2,31}/logs/[A-Za-z0-9._-]+\.log$`),
}

// adoptedLogDir matches an adopted site's logs folder (from its Layte vhost).
var adoptedLogDir = regexp.MustCompile(`^/home/[a-z][a-z0-9_-]{2,31}/logs$`)

func logAllowed(p string) bool {
	if path.Clean(p) != p {
		return false
	}
	for _, re := range allowedLogs {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

// LogSource is one log file a user can view.
type LogSource struct {
	Path   string `json:"path"`
	Label  string `json:"label"`
	Domain string `json:"domain,omitempty"` // site the log belongs to
}

// logsSources lists existing, allowed log files: nginx and PHP-FPM logs,
// logs named in nginx vhosts, and each Layte site's logs/ folder.
func (d Deps) logsSources(ctx context.Context, _ agent.Actor, _ agent.NoParams) (any, error) {
	seen := map[string]bool{}
	out := []LogSource{}
	add := func(p, label, domain string) {
		if seen[p] || !logAllowed(p) {
			return
		}
		if fi, err := d.FS.Lstat(p); err != nil || !fi.Mode().IsRegular() {
			return
		}
		seen[p] = true
		out = append(out, LogSource{Path: p, Label: label, Domain: domain})
	}

	add("/var/log/nginx/access.log", "nginx access log", "")
	add("/var/log/nginx/error.log", "nginx error log", "")
	if versions, err := d.phpVersions(); err == nil {
		for _, v := range versions {
			add("/var/log/php"+v+"-fpm.log", "PHP "+v+" FPM log", "")
		}
	}

	// Logs referenced by enabled vhosts (imported and managed sites).
	files, _ := d.vhostConfigs()
	for _, real := range files {
		b, err := d.FS.ReadFile(real)
		if err != nil {
			continue
		}
		ds, err := nginx.Parse(string(b))
		if err != nil {
			continue
		}
		for _, srv := range servers(ds) {
			site := d.siteFromServer(srv.Block, nil)
			if site == nil {
				continue
			}
			for _, name := range []string{"access_log", "error_log"} {
				if dir := nginx.First(srv.Block, name); dir != nil {
					label := strings.ReplaceAll(name, "_", " ")
					add(dir.Arg(0), label, site.Domain)
				}
			}
			// The PHP error log of the site's own PHP-FPM pool (adopted sites
			// keep theirs where it was; WP Debug writes there).
			if site.PHPSocket != "" {
				if pools := d.findPools(site.PHPSocket); len(pools) == 1 {
					if l := poolErrorLog(pools[0].Content); l != "" {
						add(l, "PHP error log", site.Domain)
					}
				}
			}
			// Everything in a Layte site's logs/ folder (php-error.log, cron.log, …):
			// next to public/, or (adopted sites Layte took over) where the
			// vhost's access log is.
			logDir := ""
			if strings.HasSuffix(site.Root, "/public") {
				logDir = path.Dir(site.Root) + "/logs"
			} else if a := nginx.First(srv.Block, "access_log"); system.IsManaged(b) && a != nil && adoptedLogDir.MatchString(path.Dir(a.Arg(0))) {
				logDir = path.Dir(a.Arg(0))
			}
			if logDir != "" {
				logs, _ := d.FS.Glob(logDir + "/*.log")
				sort.Strings(logs)
				for _, l := range logs {
					add(l, strings.TrimSuffix(path.Base(l), ".log"), site.Domain)
				}
			}
		}
	}
	return out, nil
}

type LogReadParams struct {
	Path string `json:"path"`
	// Offset to read from; -1 means "the last TailLines lines".
	Offset    int64 `json:"offset"`
	TailLines int   `json:"tail_lines,omitempty"`
}

func (p LogReadParams) Validate() error {
	if !logAllowed(p.Path) {
		return fmt.Errorf("%s is not a log file Layte Panel can show", p.Path)
	}
	if p.Offset < -1 || p.TailLines < 0 || p.TailLines > 2000 {
		return errors.New("invalid offset or tail_lines")
	}
	return nil
}

// LogChunk is new log data since an offset.
type LogChunk struct {
	Data    string `json:"data"`
	Offset  int64  `json:"offset"`  // where to continue reading
	Rotated bool   `json:"rotated"` // file shrank (rotated): reading restarted at 0
}

const maxLogChunk = 256 << 10

// logsRead returns the tail of a log, or what was appended since offset.
func (d Deps) logsRead(ctx context.Context, _ agent.Actor, p LogReadParams) (any, error) {
	real, err := d.FS.Resolve(p.Path)
	if err != nil {
		return nil, agent.Errorf(agent.CodeNotFound, "%s: %v", p.Path, err)
	}
	if !logAllowed(real) {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s points outside the allowed log locations", p.Path)
	}
	if fi, err := d.FS.Lstat(real); err != nil || !fi.Mode().IsRegular() {
		return nil, agent.Errorf(agent.CodeNotFound, "%s is not a regular file", p.Path)
	}

	if p.Offset == -1 {
		_, size, err := d.FS.ReadRange(real, 0, 0)
		if err != nil {
			return nil, err
		}
		start := size - maxLogChunk
		if start < 0 {
			start = 0
		}
		b, size, err := d.FS.ReadRange(real, start, maxLogChunk)
		if err != nil {
			return nil, err
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if start > 0 && len(lines) > 0 {
			lines = lines[1:] // first line is probably cut off
		}
		n := p.TailLines
		if n == 0 {
			n = 200
		}
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		data := strings.Join(lines, "\n")
		if data != "" {
			data += "\n"
		}
		return LogChunk{Data: data, Offset: size}, nil
	}

	chunk := LogChunk{}
	b, size, err := d.FS.ReadRange(real, p.Offset, maxLogChunk)
	if err != nil {
		return nil, err
	}
	if p.Offset > size { // truncated or rotated
		chunk.Rotated = true
		if b, _, err = d.FS.ReadRange(real, 0, maxLogChunk); err != nil {
			return nil, err
		}
		p.Offset = 0
	}
	// Only hand out complete lines; the rest comes with the next read.
	if i := strings.LastIndexByte(string(b), '\n'); i >= 0 {
		b = b[:i+1]
	} else if len(b) < maxLogChunk {
		b = nil
	}
	chunk.Data = string(b)
	chunk.Offset = p.Offset + int64(len(b))
	return chunk, nil
}
