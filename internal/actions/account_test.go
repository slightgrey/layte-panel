package actions

import (
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
)

// Logins "my" and "my-site" must stay apart: a dash would nest one slice in
// the other, and a plain glob would match the other's units.
func TestAccountNamesDontOverlap(t *testing.T) {
	d := devDeps(t)
	for _, u := range []string{"my", "my-site"} {
		if _, err := d.siteUserCreate(ctx, agent.Actor{}, UserParams{Username: u}); err != nil {
			t.Fatal(err)
		}
		if err := d.ensureAccountFPM(ctx, u, "8.3"); err != nil {
			t.Fatal(err)
		}
	}
	if got := d.acctFPMUnits("my"); len(got) != 1 || got["8.3"] != "layte-fpm-my-8.3.service" {
		t.Errorf("units of my = %v", got)
	}
	if _, err := d.accountRemove(ctx, agent.Actor{}, UserParams{Username: "my"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.FS.Exists(systemdUnitDir + "/layte-fpm-my-site-8.3.service"); !ok {
		t.Error("removing the login my removed my-site's PHP-FPM")
	}
	// Slices are named by UID, so they can never nest.
	my, site := d.acctSlice("my"), d.acctSlice("my-site")
	if my == "" || my == site || strings.Count(site, "-") != 2 {
		t.Errorf("slices %q and %q", my, site)
	}
	b, _ := d.FS.ReadFile(systemdUnitDir + "/layte-fpm-my-site-8.3.service")
	if !strings.Contains(string(b), "Slice="+site+"\n") {
		t.Errorf("unit:\n%s", b)
	}
}
