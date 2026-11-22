package actions

import (
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

func TestSiteDiskUsage(t *testing.T) {
	d := devDeps(t)
	// Imported site: whole web root only, no breakdown.
	out, err := d.siteDiskUsage(ctx, agent.Actor{}, SiteDiskParams{Domain: "blog.example.com", WebRoot: "/var/www/blog"})
	if err != nil {
		t.Fatal(err)
	}
	u := out.(DiskUsage)
	if u.Path != "/var/www/blog" || u.Total == 0 || len(u.Parts) != 0 {
		t.Errorf("imported: %+v", u)
	}
	// Another site's web root is refused, like the file manager.
	if _, err := d.siteDiskUsage(ctx, agent.Actor{}, SiteDiskParams{Domain: "blog.example.com", WebRoot: "/var/www/shop/public"}); err == nil {
		t.Error("measured another site's web root")
	}
	for _, bad := range []SiteDiskParams{
		{Domain: "a.test"},
		{Domain: "a.test", WebRoot: "/etc"},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestDiskUsageToleratesVanishedFiles(t *testing.T) {
	d := devDeps(t)
	d.Runner = &system.FakeRunner{Handle: func(c system.Cmd) (system.Result, error) {
		return system.Result{Stdout: []byte("1234\t/var/www/blog\n"), Stderr: []byte("du: cannot access 'x': No such file")},
			&system.ExitError{Bin: c.Bin, ExitCode: 1}
	}}
	out, err := d.siteDiskUsage(ctx, agent.Actor{}, SiteDiskParams{Domain: "blog.example.com", WebRoot: "/var/www/blog"})
	if err != nil || out.(DiskUsage).Total != 1234 {
		t.Errorf("got %+v, %v", out, err)
	}
}
