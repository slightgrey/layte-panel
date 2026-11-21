package actions

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

func TestCloudflareRangesAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range cloudflareRanges {
		p, err := netip.ParsePrefix(r)
		if err != nil || p.Masked() != p {
			t.Errorf("bad range %q: %v", r, err)
		}
		if seen[r] {
			t.Errorf("duplicate range %q", r)
		}
		seen[r] = true
	}
	if len(cloudflareRanges) < 10 {
		t.Errorf("only %d ranges", len(cloudflareRanges))
	}
}

// cfDeps is a dev FS with an Ubuntu-style nginx.conf (includes conf.d).
func cfDeps(t *testing.T) Deps {
	d := devDeps(t)
	d.FS.WriteFileAtomic("/etc/nginx/nginx.conf", []byte("user www-data;\nhttp {\n    include /etc/nginx/conf.d/*.conf;\n    include /etc/nginx/sites-enabled/*;\n}\n"), 0o644)
	d.FS.MkdirAll("/etc/nginx/conf.d", 0o755)
	return d
}

func TestCloudflareEnableUpdateDisable(t *testing.T) {
	d := cfDeps(t)
	if st := d.cloudflareStatus(); st.Enabled || st.Problem != "" || len(st.Conflicts) > 0 {
		t.Fatalf("initial status = %+v", st)
	}

	out, err := d.cloudflareSet(ctx, agent.Actor{}, CloudflareParams{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if st := out.(CloudflareStatus); !st.Enabled || !st.UpToDate || st.Ranges != len(cloudflareRanges) {
		t.Errorf("after enable = %+v", st)
	}
	b, _ := d.FS.ReadFile(cloudflareConf)
	if !system.IsManaged(b) || !strings.Contains(string(b), "set_real_ip_from 173.245.48.0/20;\n") ||
		!strings.HasSuffix(string(b), "real_ip_header CF-Connecting-IP;\n") {
		t.Errorf("file:\n%s", b)
	}
	fr := d.Runner.(*system.FakeRunner)
	var cmds []string
	for _, c := range fr.Cmds {
		cmds = append(cmds, c.String())
	}
	if !slices.Contains(cmds, nginxBin+" -t") || !slices.Contains(cmds, systemctl+" reload nginx") {
		t.Errorf("cmds = %v", cmds)
	}

	// An older list (e.g. written by a previous version) is reported, and
	// enabling again rewrites it.
	d.FS.WriteFileAtomic(cloudflareConf, []byte(system.Marker+" — old list\nset_real_ip_from 1.2.3.0/24;\nreal_ip_header CF-Connecting-IP;\n"), 0o644)
	if st := d.cloudflareStatus(); !st.Enabled || st.UpToDate {
		t.Errorf("old list status = %+v", st)
	}
	if _, err := d.cloudflareSet(ctx, agent.Actor{}, CloudflareParams{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if st := d.cloudflareStatus(); !st.UpToDate {
		t.Errorf("after update = %+v", st)
	}

	if _, err := d.cloudflareSet(ctx, agent.Actor{}, CloudflareParams{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.FS.Exists(cloudflareConf); ok {
		t.Error("file not removed")
	}
	// Disabling again is a no-op.
	if _, err := d.cloudflareSet(ctx, agent.Actor{}, CloudflareParams{Enabled: false}); err != nil {
		t.Error(err)
	}
}

func TestCloudflareConflicts(t *testing.T) {
	tests := []struct {
		name, file, content string
		conflict            bool
	}{
		{"hand-made http-level file", "/etc/nginx/conf.d/cloudflare-realip.conf",
			"# Real visitor IPs\nset_real_ip_from 173.245.48.0/20;\nreal_ip_header CF-Connecting-IP;\n", true},
		{"inside one site's server block", "/etc/nginx/conf.d/blog.conf",
			"server {\n    server_name blog.test;\n    set_real_ip_from 173.245.48.0/20;\n    real_ip_header CF-Connecting-IP;\n}\n", false},
		{"commented out", "/etc/nginx/conf.d/old.conf", "# real_ip_header CF-Connecting-IP;\n", false},
		{"nginx.conf http block", "/etc/nginx/nginx.conf",
			"http {\n    real_ip_header X-Forwarded-For;\n    include /etc/nginx/conf.d/*.conf;\n}\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := cfDeps(t)
			d.FS.WriteFileAtomic(tt.file, []byte(tt.content), 0o644)
			st := d.cloudflareStatus()
			if got := slices.Contains(st.Conflicts, tt.file); got != tt.conflict {
				t.Fatalf("conflicts = %v, want %s listed: %v", st.Conflicts, tt.file, tt.conflict)
			}
			_, err := d.cloudflareSet(ctx, agent.Actor{}, CloudflareParams{Enabled: true})
			if tt.conflict && !agent.IsCode(err, agent.CodeInvalidParams) {
				t.Errorf("enable with a conflict: %v", err)
			}
			if !tt.conflict && err != nil {
				t.Errorf("enable: %v", err)
			}
		})
	}
}

func TestCloudflareNeedsConfDInclude(t *testing.T) {
	d := cfDeps(t)
	d.FS.WriteFileAtomic("/etc/nginx/nginx.conf", []byte("http {\n    include /etc/nginx/sites-enabled/*;\n}\n"), 0o644)
	if st := d.cloudflareStatus(); st.Problem == "" {
		t.Errorf("status = %+v", st)
	}
	if _, err := d.cloudflareSet(ctx, agent.Actor{}, CloudflareParams{Enabled: true}); !agent.IsCode(err, agent.CodeInvalidParams) {
		t.Errorf("enable: %v", err)
	}
}
