package actions

import (
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

func TestSitesDiscover(t *testing.T) {
	d := devDeps(t)
	out, err := d.sitesDiscover(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		t.Fatal(err)
	}
	sites := out.([]DiscoveredSite)
	by := map[string]DiscoveredSite{}
	for _, s := range sites {
		by[s.Domain] = s
	}
	if len(sites) != 3 {
		t.Fatalf("got %d sites: %+v", len(sites), sites)
	}

	blog := by["blog.example.com"]
	if !blog.SSL || blog.PHPVersion != "8.3" || blog.Root != "/var/www/blog" || !blog.WordPress || blog.Managed ||
		len(blog.Aliases) != 1 || blog.Aliases[0] != "www.blog.example.com" ||
		blog.CertPath != "/etc/letsencrypt/live/blog.example.com/fullchain.pem" {
		t.Errorf("blog = %+v", blog)
	}
	// PHP version found via the custom pool's listen socket.
	if shop := by["shop.example.org"]; shop.PHPVersion != "8.2" || shop.SSL || shop.WordPress {
		t.Errorf("shop = %+v", shop)
	}
	if st := by["static.example.net"]; st.PHPVersion != "" || st.Root != "/var/www/static" {
		t.Errorf("static = %+v", st)
	}
	if _, ok := by["_"]; ok {
		t.Error("default server listed")
	}
}

func TestSitesDiscoverIgnoresLinksOutsideNginx(t *testing.T) {
	d := devDeps(t)
	d.FS.WriteFileAtomic("/etc/secret.conf", []byte("server { server_name leak.example.com; }"), 0o600)
	d.FS.Symlink("/etc/secret.conf", "/etc/nginx/sites-enabled/leak")
	out, _ := d.sitesDiscover(ctx, agent.Actor{}, agent.NoParams{})
	for _, s := range out.([]DiscoveredSite) {
		if s.Domain == "leak.example.com" {
			t.Error("followed a symlink outside /etc/nginx")
		}
	}
	if _, err := d.vhostRead(ctx, agent.Actor{}, VhostReadParams{Path: "/etc/nginx/sites-enabled/leak"}); err == nil {
		t.Error("vhost_read followed a symlink outside /etc/nginx")
	}
}

func TestVhostRead(t *testing.T) {
	d := devDeps(t)
	out, err := d.vhostRead(ctx, agent.Actor{}, VhostReadParams{Path: "/etc/nginx/sites-enabled/static.example.net"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["managed"] != false || len(m["content"].(string)) == 0 {
		t.Errorf("got %v", m)
	}
	for _, bad := range []string{"/etc/passwd", "/etc/nginx/../shadow", "etc/nginx/x", "/etc/nginx"} {
		if (VhostReadParams{Path: bad}).Validate() == nil {
			t.Errorf("Validate(%q) accepted", bad)
		}
	}
}

func TestDiscoverMarksManaged(t *testing.T) {
	d := devDeps(t)
	d.FS.WriteFileAtomic("/etc/nginx/sites-enabled/layte-new.test.conf",
		[]byte(system.Marker+"\nserver { listen 80; server_name new.test; root /home/newtest/new.test/public; }\n"), 0o644)
	out, _ := d.sitesDiscover(ctx, agent.Actor{}, agent.NoParams{})
	for _, s := range out.([]DiscoveredSite) {
		if s.Domain == "new.test" && !s.Managed {
			t.Error("Layte-managed file not marked managed")
		}
	}
}
