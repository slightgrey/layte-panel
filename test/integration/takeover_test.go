//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/phpini"
	"github.com/slightgrey/layte-panel/internal/store"
)

// TestTakeoverAdopted lets Layte manage the nginx config of a site that was
// already adopted in the VM (it changes that site; see docs/troubleshooting.md
// to give the config back). Opt in with
//
//	LAYTE_IT_TAKEOVER=<domain>:<user>:<web root>:<php version>
func TestTakeoverAdopted(t *testing.T) {
	spec := os.Getenv("LAYTE_IT_TAKEOVER")
	if spec == "" {
		t.Skip("set LAYTE_IT_TAKEOVER=<domain>:<user>:<web root>:<php version>")
	}
	f := strings.Split(spec, ":")
	if len(f) != 4 {
		t.Fatal("LAYTE_IT_TAKEOVER needs 4 fields")
	}
	e := setup(t)
	ctx := context.Background()
	id, err := e.st.CreateSite(ctx, store.Site{Domain: f[0], SystemUser: f[1], RootPath: f[2], PHPVersion: f[3],
		Source: "imported", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	site, _ := e.st.SiteByID(ctx, id)
	site.Adopted = true
	e.st.UpdateSite(ctx, site)
	before := get(t, f[0])

	p, err := e.svc.TakeoverPreview(ctx, e.actor, site)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("preview: vhost %s → %s (kept as %q), pool %s → %s, warnings %v, blockers %v",
		p.VhostFile, p.NewVhostFile, p.KeptAs, p.PoolFile, p.NewPoolFile, p.Warnings, p.Blockers)
	if len(p.Blockers) > 0 {
		t.Fatal("blocked")
	}
	jobID, err := e.svc.Takeover(ctx, e.actor, site, p.VhostHash, "")
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
	nginxTestOK(t)
	if after := get(t, f[0]); after != before {
		t.Errorf("site answers differently: before %q, after %q", before, after)
	}
	site, _ = e.st.SiteByID(ctx, id)
	if !site.NginxManaged || !site.LayteConfigs() {
		t.Fatalf("site after takeover: %+v", site)
	}

	// PHP settings now rewrite the Layte pool and vhost (web root kept).
	jobID, err = e.svc.SetPHPSettings(ctx, e.actor, site, phpini.Settings{UploadMax: "128M", MaxExecutionTime: 120})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(t, jobID)
	nginxTestOK(t)
	vhost, _ := os.ReadFile(site.NginxConf)
	if !strings.Contains(string(vhost), "client_max_body_size 128m;") || !strings.Contains(string(vhost), "root "+f[2]+";") {
		t.Errorf("vhost after PHP settings:\n%s", vhost)
	}
	if after := get(t, f[0]); after != before {
		t.Errorf("site answers differently after PHP settings")
	}
}
