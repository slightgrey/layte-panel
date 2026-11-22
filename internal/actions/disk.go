package actions

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

const duBin = "/usr/bin/du"

// SiteDiskParams address a site's folder the same way as the file manager:
// Layte sites by Username, imported sites by WebRoot.
type SiteDiskParams struct {
	Domain   string `json:"domain"`
	Username string `json:"username,omitempty"`
	WebRoot  string `json:"web_root,omitempty"`
}

func (p SiteDiskParams) Validate() error {
	return FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot, Op: "stat", Path: "."}.validate(readOps)
}

// DiskPart is the size of one folder.
type DiskPart struct {
	Name  string `json:"name"` // "public", "logs", "tmp"; "" for the whole site
	Bytes int64  `json:"bytes"`
}

// DiskUsage is a site's size on disk.
type DiskUsage struct {
	Path  string     `json:"path"`
	Total int64      `json:"total"`
	Parts []DiskPart `json:"parts,omitempty"`
}

// siteDiskUsage measures the site's folder with du (read-only). Layte sites
// also get a public/logs/tmp breakdown. du doesn't follow symlinks and
// --one-file-system keeps it from wandering into mounts.
func (d Deps) siteDiskUsage(ctx context.Context, _ agent.Actor, p SiteDiskParams) (any, error) {
	root, _, _, err := d.jail(ctx, FSParams{Domain: p.Domain, Username: p.Username, WebRoot: p.WebRoot})
	if err != nil {
		return nil, err
	}
	paths := []string{root}
	var parts []string
	if p.Username != "" {
		for _, sub := range []string{"public", "logs", "tmp"} {
			if fi, err := d.FS.Lstat(root + "/" + sub); err == nil && fi.IsDir() {
				parts = append(parts, sub)
				paths = append(paths, root+"/"+sub)
			}
		}
	}
	args := append([]string{"--summarize", "--block-size=1", "--one-file-system", "--"}, paths...)
	res, err := d.Runner.Run(ctx, duBin, args...)
	sizes := parseDu(string(res.Stdout))
	// du exits 1 when a file vanishes mid-walk; its totals are still usable.
	var ee *system.ExitError
	if err != nil && !(errors.As(err, &ee) && len(sizes) > 0) {
		return nil, agent.Errorf("du_failed", "du: %s", failure(res, err, 3))
	}
	out := DiskUsage{Path: root, Total: sizes[root]}
	for _, sub := range parts {
		out.Parts = append(out.Parts, DiskPart{Name: sub, Bytes: sizes[root+"/"+sub]})
	}
	return out, nil
}

// parseDu reads "<bytes>\t<path>" lines.
func parseDu(out string) map[string]int64 {
	m := map[string]int64{}
	for _, line := range strings.Split(out, "\n") {
		n, p, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		if v, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
			m[p] = v
		}
	}
	return m
}
