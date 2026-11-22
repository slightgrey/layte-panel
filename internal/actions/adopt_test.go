package actions

import (
	"context"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

const shopPool = "/etc/php/8.2/fpm/pool.d/shop.conf"

func TestAdoptPreview(t *testing.T) {
	d := devDeps(t)
	out, err := d.adoptPreview(ctx, agent.Actor{}, AdoptPreviewParams{Domain: "shop.example.org", WebRoot: "/var/www/shop/public"})
	if err != nil {
		t.Fatal(err)
	}
	p := out.(AdoptPreview)
	if p.PoolFile != shopPool || p.OldUser != "shop" || p.PHPVersion != "8.2" || len(p.Blockers) != 0 || p.PoolHash == "" {
		t.Errorf("preview: %+v", p)
	}
	diff := strings.Join(p.PoolDiff, "\n")
	if !strings.Contains(diff, "- user = shop") || !strings.Contains(diff, "+ user = <new user>") || !strings.Contains(diff, "+ group = <new user>") {
		t.Errorf("diff:\n%s", diff)
	}

	// The blog uses PHP's default www pool: never adoptable.
	out, err = d.adoptPreview(ctx, agent.Actor{}, AdoptPreviewParams{Domain: "blog.example.com", WebRoot: "/var/www/blog"})
	if err != nil {
		t.Fatal(err)
	}
	if b := out.(AdoptPreview).Blockers; len(b) == 0 || !strings.Contains(b[0], "default pool") {
		t.Errorf("blog blockers: %v", b)
	}
	// Static site: no pool. Wrong web root: not found.
	if _, err := d.adoptPreview(ctx, agent.Actor{}, AdoptPreviewParams{Domain: "static.example.net", WebRoot: "/var/www/static"}); err == nil {
		t.Error("static site has no pool to adopt")
	}
	if _, err := d.adoptPreview(ctx, agent.Actor{}, AdoptPreviewParams{Domain: "shop.example.org", WebRoot: "/var/www/blog"}); err == nil {
		t.Error("accepted another site's web root")
	}
}

func adoptShop(t *testing.T, d Deps) (any, error) {
	t.Helper()
	out, err := d.adoptPreview(ctx, agent.Actor{}, AdoptPreviewParams{Domain: "shop.example.org", WebRoot: "/var/www/shop/public"})
	if err != nil {
		t.Fatal(err)
	}
	return d.siteAdopt(ctx, agent.Actor{}, AdoptParams{Domain: "shop.example.org", WebRoot: "/var/www/shop/public",
		Username: "shopexampleorg", PoolHash: out.(AdoptPreview).PoolHash})
}

func TestSiteAdopt(t *testing.T) {
	d := devDeps(t)
	orig, _ := d.FS.ReadFile(shopPool)
	out, err := adoptShop(t, d)
	if err != nil {
		t.Fatal(err)
	}
	if r := out.(AdoptResult); r.Username != "shopexampleorg" || r.OldUser != "shop" || len(r.Steps) < 4 {
		t.Errorf("result: %+v", r)
	}
	pool, _ := d.FS.ReadFile(shopPool)
	if !system.IsManaged(pool) || !strings.Contains(string(pool), "\nuser = shopexampleorg\n") || !strings.Contains(string(pool), "\ngroup = shopexampleorg\n") ||
		!strings.Contains(string(pool), "listen = /run/php/shop.sock") {
		t.Errorf("pool after adopt:\n%s", pool)
	}
	if kept, _ := d.FS.ReadFile(shopPool + system.AdoptSuffix); string(kept) != string(orig) {
		t.Error("original pool not kept")
	}
	for _, p := range []string{"/home/shopexampleorg/" + siteUserMarker, "/home/shopexampleorg/tmp", "/home/shopexampleorg/logs"} {
		if ok, _ := d.FS.Exists(p); !ok {
			t.Errorf("missing %s", p)
		}
	}
	var chowns []string
	for _, c := range d.Runner.(*system.FakeRunner).CallList() {
		if strings.HasPrefix(c, chownBin) {
			chowns = append(chowns, c)
		}
	}
	if len(chowns) < 2 || !strings.Contains(chowns[0], "--no-dereference --from=shop shopexampleorg -- /var/www/shop/public") ||
		!strings.Contains(chowns[1], "--from=:shop :shopexampleorg") {
		t.Errorf("chown calls: %q", chowns)
	}
	// Adopting again: the pool is now Layte's.
	if _, err := adoptShop(t, d); err == nil {
		t.Error("adopted twice")
	}
}

func TestSiteAdoptRollsBack(t *testing.T) {
	d := devDeps(t)
	orig, _ := d.FS.ReadFile(shopPool)
	calls := 0
	d.SiteCheck = func(context.Context, string, bool) (int, error) {
		calls++
		if calls == 1 {
			return 200, nil
		}
		return 502, nil // PHP broke after the switch
	}
	if _, err := adoptShop(t, d); err == nil || !strings.Contains(err.Error(), "undone") {
		t.Fatalf("expected rollback, got %v", err)
	}
	if pool, _ := d.FS.ReadFile(shopPool); string(pool) != string(orig) {
		t.Errorf("pool not restored:\n%s", pool)
	}
	if ok, _ := d.FS.Exists(shopPool + system.AdoptSuffix); ok {
		t.Error("original copy left behind")
	}
	if ok, _ := d.FS.Exists("/home/shopexampleorg"); ok {
		t.Error("new user not removed")
	}
	var back bool
	for _, c := range d.Runner.(*system.FakeRunner).CallList() {
		back = back || strings.Contains(c, "--from=shopexampleorg shop -- /var/www/shop/public")
	}
	if !back {
		t.Error("files not given back to the old owner")
	}
}

func TestSiteAdoptRefusesChangedPool(t *testing.T) {
	d := devDeps(t)
	_, err := d.siteAdopt(ctx, agent.Actor{}, AdoptParams{Domain: "shop.example.org", WebRoot: "/var/www/shop/public",
		Username: "shopexampleorg", PoolHash: "0000000000000000"})
	if err == nil || !strings.Contains(err.Error(), "changed since the preview") {
		t.Errorf("got %v", err)
	}
	for _, bad := range []AdoptParams{
		{Domain: "shop.example.org", WebRoot: "/var/www/shop/public", Username: "root", PoolHash: "x"},
		{Domain: "shop.example.org", WebRoot: "/etc", Username: "shopx", PoolHash: "x"},
		{Domain: "shop.example.org", WebRoot: "/var/www/shop/public", Username: "shopx"},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestAdoptedFilesAndCron(t *testing.T) {
	d := devDeps(t)
	if _, err := adoptShop(t, d); err != nil {
		t.Fatal(err)
	}
	web := FSParams{Domain: "shop.example.org", Username: "shopexampleorg", WebRoot: "/var/www/shop/public"}
	w := web
	w.Op, w.Path = "mkdir", "uploads"
	if _, err := d.fsWrite(ctx, agent.Actor{}, FSWriteParams(w)); err != nil {
		t.Fatalf("write to adopted web root: %v", err)
	}
	if ok, _ := d.FS.Exists("/var/www/shop/public/uploads"); !ok {
		t.Error("mkdir didn't land in the web root")
	}
	data := web
	data.Op, data.Path, data.Area = "list", "tmp", "data"
	if _, err := d.fsRead(ctx, agent.Actor{}, FSReadParams(data)); err != nil {
		t.Errorf("list data area: %v", err)
	}
	// Another user name, or another web root, is refused.
	for _, bad := range []FSParams{
		{Domain: "shop.example.org", Username: "shop", WebRoot: "/var/www/shop/public", Op: "list"},
		{Domain: "shop.example.org", Username: "shopexampleorg", WebRoot: "/var/www/blog", Op: "list"},
		{Domain: "blog.example.com", Username: "shopexampleorg", WebRoot: "/var/www/blog", Op: "list"},
	} {
		if _, err := d.fsRead(ctx, agent.Actor{}, FSReadParams(bad)); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}

	// Cron runs as the new user from the web root and logs outside it.
	_, err := d.cronWrite(ctx, agent.Actor{}, CronWriteParams{Domain: "shop.example.org", Username: "shopexampleorg", WebRoot: "/var/www/shop/public",
		Jobs: []CronEntry{{Schedule: "*/5 * * * *", Command: "php artisan schedule:run"}}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := d.FS.ReadFile(CronPath("shop.example.org"))
	if !strings.Contains(string(b), "*/5 * * * * shopexampleorg cd /var/www/shop/public && ( php artisan schedule:run ) >> /home/shopexampleorg/logs/cron.log 2>&1") {
		t.Errorf("cron file:\n%s", b)
	}
	if _, err := d.cronWrite(ctx, agent.Actor{}, CronWriteParams{Domain: "shop.example.org", Username: "shop", WebRoot: "/var/www/shop/public",
		Jobs: []CronEntry{{Schedule: "* * * * *", Command: "id"}}}); err == nil {
		t.Error("cron as the old (non-Layte) user")
	}
}

func TestRenewAdoptedCertChecks(t *testing.T) {
	d := devDeps(t)
	if _, err := adoptShop(t, d); err != nil {
		t.Fatal(err)
	}
	base := SSLRenewParams{Domain: "shop.example.org", DryRun: true, Username: "shopexampleorg", WebRoot: "/var/www/shop/public"}
	// The shop vhost uses no certificate at all, so any name is refused.
	for _, name := range []string{"shop.example.org", "blog.example.com"} {
		p := base
		p.CertName = name
		if _, err := d.sslRenew(ctx, agent.Actor{}, p); err == nil {
			t.Errorf("renewed %s, which shop's vhost doesn't use", name)
		}
	}
	// Not adopted (the blog): refused before certbot runs.
	p := SSLRenewParams{Domain: "blog.example.com", DryRun: true, CertName: "blog.example.com", Username: "blogx", WebRoot: "/var/www/blog"}
	if _, err := d.sslRenew(ctx, agent.Actor{}, p); err == nil {
		t.Error("renewed a site that isn't adopted")
	}
	for _, bad := range []SSLRenewParams{
		{Domain: "shop.example.org", CertName: "../etc", Username: "shopexampleorg", WebRoot: "/var/www/shop/public"},
		{Domain: "shop.example.org", CertName: "x"},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
