package store

import (
	"context"
	"testing"
)

// A scan follows an adopted site's vhost but keeps its user and pool, and
// never deletes it.
func TestScanKeepsAdoptedSites(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	if _, _, err := s.ReplaceImported(ctx, []Site{{Domain: "blog.test", RootPath: "/var/www/blog", PHPVersion: "8.3"}}); err != nil {
		t.Fatal(err)
	}
	site, err := s.SiteByDomain(ctx, "blog.test")
	if err != nil {
		t.Fatal(err)
	}
	site.Adopted, site.SystemUser, site.FPMPool, site.PHPVersion = true, "blogtest", "/etc/php/8.3/fpm/pool.d/blog.conf", "8.3"
	if err := s.UpdateSite(ctx, site); err != nil {
		t.Fatal(err)
	}

	// Rescan with a changed vhost (now HTTPS, new alias, no user info).
	if _, _, err := s.ReplaceImported(ctx, []Site{{Domain: "blog.test", Aliases: []string{"www.blog.test"}, RootPath: "/var/www/blog", SSL: true}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.SiteByDomain(ctx, "blog.test")
	if !got.Adopted || got.SystemUser != "blogtest" || got.FPMPool == "" || got.PHPVersion != "8.3" || !got.SSL || len(got.Aliases) != 1 {
		t.Errorf("after rescan: %+v", got)
	}

	// The vhost disappears: still listed.
	if _, removed, err := s.ReplaceImported(ctx, nil); err != nil || removed != 0 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err := s.SiteByDomain(ctx, "blog.test"); err != nil {
		t.Error("adopted site dropped by a scan")
	}
}
