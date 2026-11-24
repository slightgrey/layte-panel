package store

import (
	"context"
	"testing"

	"github.com/slightgrey/layte-panel/internal/phpini"
)

func TestSitePHPSettings(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	id, err := st.CreateSite(ctx, Site{Domain: "a.test", SystemUser: "atest", PHPVersion: "8.3", Managed: true, Source: "created", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	site, _ := st.SiteByID(ctx, id)
	if site.PHPSettings != (phpini.Settings{}) {
		t.Errorf("new site: %+v", site.PHPSettings)
	}
	site.PHPSettings = phpini.Settings{MemoryLimit: "512M", UploadMax: "128M", MaxExecutionTime: 120, MaxInputVars: 5000}
	if err := st.UpdateSite(ctx, site); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.SiteByID(ctx, id); got.PHPSettings != site.PHPSettings {
		t.Errorf("saved %+v, got %+v", site.PHPSettings, got.PHPSettings)
	}
}
