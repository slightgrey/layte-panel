package actions

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

// "Behind Cloudflare": one nginx file, loaded for every site, that takes the
// visitor's IP from Cloudflare's CF-Connecting-IP header. nginx only trusts
// that header on requests coming from Cloudflare's own address ranges, so a
// visitor who reaches the server directly can't fake an IP. That makes it
// safe to turn on server-wide, also for sites that don't use the proxy.

const cloudflareConf = nginxDir + "/conf.d/layte-cloudflare-realip.conf"

// cloudflareRanges is https://www.cloudflare.com/ips/ as of cloudflareListDate.
// The list ships with each release (the root agent never downloads config);
// after an upgrade with a newer list, the UI offers to update the file.
const cloudflareListDate = "2026-11-22"

var cloudflareRanges = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
}

// CloudflareStatus is shown on the Security page.
type CloudflareStatus struct {
	Enabled   bool     `json:"enabled"`             // Layte's file is in place
	UpToDate  bool     `json:"up_to_date"`          // it matches this version's list
	Ranges    int      `json:"ranges"`              // ranges in this version's list
	ListDate  string   `json:"list_date"`           // date of this version's list
	Conflicts []string `json:"conflicts,omitempty"` // other files that already set real_ip_header for all sites
	Problem   string   `json:"problem,omitempty"`   // why it can't be turned on here
}

type CloudflareParams struct {
	Enabled bool `json:"enabled"`
}

func (CloudflareParams) Validate() error { return nil }

func cloudflareContent() []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s — do not edit by hand (Behind Cloudflare: real visitor IPs)\n", system.Marker)
	fmt.Fprintf(&b, "# Cloudflare's address ranges as of %s (https://www.cloudflare.com/ips/).\n", cloudflareListDate)
	b.WriteString("# The header is only trusted on requests from these ranges.\n")
	for _, r := range cloudflareRanges {
		fmt.Fprintf(&b, "set_real_ip_from %s;\n", r)
	}
	b.WriteString("real_ip_header CF-Connecting-IP;\n")
	return b.Bytes()
}

var confDInclude = regexp.MustCompile(`(?m)^[ \t]*include[ \t]+(/etc/nginx/)?conf\.d/\*\.conf`)

func (d Deps) cloudflareStatus() CloudflareStatus {
	st := CloudflareStatus{Ranges: len(cloudflareRanges), ListDate: cloudflareListDate}
	if b, err := d.FS.ReadFile(cloudflareConf); err == nil && system.IsManaged(b) {
		st.Enabled = true
		st.UpToDate = bytes.Equal(b, cloudflareContent())
	}
	st.Conflicts = d.realIPConflicts()
	if b, err := d.FS.ReadFile(nginxDir + "/nginx.conf"); err == nil && !confDInclude.Match(b) {
		st.Problem = "nginx.conf doesn't include conf.d/*.conf"
	}
	return st
}

// realIPConflicts lists config files (not Layte's) that set real_ip_header
// for all sites: outside any server block in conf.d, or in nginx.conf's http
// block. A second one makes `nginx -t` fail ("directive is duplicate").
// Settings inside a single site's server block are fine and not listed.
func (d Deps) realIPConflicts() []string {
	var out []string
	files, _ := d.FS.Glob(nginxDir + "/conf.d/*.conf")
	for _, f := range files {
		if f != cloudflareConf && hasRealIPHeaderAtDepth(d.FS, f, 0) {
			out = append(out, f)
		}
	}
	if hasRealIPHeaderAtDepth(d.FS, nginxDir+"/nginx.conf", 1) {
		out = append(out, nginxDir+"/nginx.conf")
	}
	return out
}

// hasRealIPHeaderAtDepth reports whether the file has a real_ip_header line
// nested at most maxDepth braces deep (comments ignored).
func hasRealIPHeaderAtDepth(fsys system.FS, file string, maxDepth int) bool {
	b, err := fsys.ReadFile(file)
	if err != nil {
		return false
	}
	depth := 0
	for line := range strings.SplitSeq(string(b), "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if f := strings.Fields(line); len(f) > 0 && f[0] == "real_ip_header" && depth <= maxDepth {
			return true
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
	}
	return false
}

// cloudflareSet turns "Behind Cloudflare" on (or rewrites the file with this
// version's list) or off. Both go through the safe config write: nginx -t,
// reload, and a rollback if nginx rejects it.
func (d Deps) cloudflareSet(ctx context.Context, _ agent.Actor, p CloudflareParams) (any, error) {
	if !p.Enabled {
		if ok, _ := d.FS.Exists(cloudflareConf); ok {
			err := system.RemoveConfig(ctx, d.FS, d.Runner, system.RemoveSpec{Path: cloudflareConf, Test: nginxTest, Reload: nginxReload})
			if err != nil {
				return nil, configError(err)
			}
		}
		return d.cloudflareStatus(), nil
	}
	st := d.cloudflareStatus()
	if st.Problem != "" {
		return nil, agent.Errorf(agent.CodeInvalidParams, "%s", st.Problem)
	}
	if len(st.Conflicts) > 0 {
		return nil, agent.Errorf(agent.CodeInvalidParams,
			"real visitor IPs are already set up in %s; remove that first to let Layte Panel manage it", strings.Join(st.Conflicts, ", "))
	}
	err := system.WriteConfig(ctx, d.FS, d.Runner, system.WriteSpec{
		Path: cloudflareConf, Content: cloudflareContent(), Test: nginxTest, Reload: nginxReload,
	})
	if err != nil {
		return nil, configError(err)
	}
	return d.cloudflareStatus(), nil
}
