package actions

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

func newUpdates(d Deps) *updates { return &updates{deps: d} }

func TestParseSimulation(t *testing.T) {
	d := devDeps(t)
	plan, err := newUpdates(d).simulate(ctx, UpgradeParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Upgrades) != 3 || len(plan.NewPackages) != 0 || len(plan.Removals) != 0 {
		t.Fatalf("plan: %+v", plan)
	}
	if !slices.Equal(plan.KeptBack, []string{"libgd-dev", "libgd3"}) {
		t.Errorf("kept back: %v", plan.KeptBack)
	}
	var openssl PlannedPkg
	for _, p := range plan.Upgrades {
		if p.Name == "openssl" {
			openssl = p
		}
	}
	if openssl.From != "3.0.13-0ubuntu3.4" || openssl.To != "3.0.13-0ubuntu3.5" || !openssl.Security {
		t.Errorf("openssl: %+v", openssl)
	}
	if !slices.Contains(plan.Restarts, "nginx") || plan.RebootLikely || plan.Hash == "" {
		t.Errorf("restarts=%v reboot=%v hash=%q", plan.Restarts, plan.RebootLikely, plan.Hash)
	}

	// Kept-back packages on request: their new dependency is listed.
	kept, err := newUpdates(d).simulate(ctx, UpgradeParams{Packages: []string{"libgd3", "libgd-dev"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(kept.Upgrades) != 2 || len(kept.NewPackages) != 1 || kept.NewPackages[0].Name != "libheif1" || kept.Hash == plan.Hash {
		t.Errorf("kept-back plan: %+v", kept)
	}
	// A package with nothing to upgrade is refused.
	if _, err := newUpdates(d).simulate(ctx, UpgradeParams{Packages: []string{"nginx"}}); err == nil {
		t.Error("accepted a package that isn't in the plan")
	}
	for _, bad := range []UpgradeParams{{Packages: []string{"-o"}}, {Packages: []string{"a;rm"}}, {Packages: []string{"A"}}} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if (UpgradeApplyParams{}).Validate() == nil {
		t.Error("apply without plan hash accepted")
	}
	if !rebootPackage("linux-image-6.8.0-50-generic") || rebootPackage("nginx") {
		t.Error("rebootPackage")
	}
	if s := serviceFor("php8.4-fpm"); len(s) != 1 || s[0] != "php8.4-fpm" {
		t.Errorf("serviceFor php: %v", s)
	}
}

func TestUpdatesApply(t *testing.T) {
	d := devDeps(t)
	u := newUpdates(d)
	plan, _ := u.simulate(ctx, UpgradeParams{})

	if _, err := u.updatesApply(ctx, agent.Actor{}, UpgradeApplyParams{PlanHash: "0000"}); err == nil || !strings.Contains(err.Error(), "changed since the preview") {
		t.Fatalf("stale plan: %v", err)
	}
	d.FS.MkdirAll("/run", 0o755)
	d.FS.WriteFileAtomic(rebootFlagPath, []byte("*** System restart required ***\n"), 0o644)
	d.FS.WriteFileAtomic(rebootPkgsPath, []byte("linux-image-6.8.0-50-generic\n"), 0o644)
	out, err := u.updatesApply(ctx, agent.Actor{}, UpgradeApplyParams{PlanHash: plan.Hash})
	if err != nil {
		t.Fatal(err)
	}
	r := out.(UpgradeResult)
	if r.Installed != 3 || !r.RebootRequired || len(r.RebootPackages) != 1 || len(r.Sites) == 0 || r.SitesChanged != 0 {
		t.Errorf("result: %+v", r)
	}
	// The real install runs outside the agent's sandbox, through systemd-run;
	// only the simulation runs apt-get directly.
	var run system.Cmd
	for _, c := range d.Runner.(*system.FakeRunner).Cmds {
		if c.Bin == aptGetBin && !slices.Contains(c.Args, "--simulate") {
			t.Errorf("apt-get ran inside the agent's sandbox: %s", c)
		}
		if c.Bin == systemdRunBin && slices.Contains(c.Args, aptGetBin) {
			run = c
		}
	}
	if !slices.Contains(run.Args, "--wait") || !slices.Contains(run.Args, "--pipe") {
		t.Errorf("systemd-run must wait and pipe output: %s", run)
	}
	cmd := run.String()
	for _, want := range []string{"-y", "Dpkg::Options::=--force-confold", "Dpkg::Options::=--force-confdef", "DPkg::Lock::Timeout=", " upgrade"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("apt-get command lacks %q: %s", want, cmd)
		}
	}
	for _, never := range []string{"full-upgrade", "dist-upgrade", "autoremove", "remove", "purge"} {
		if strings.Contains(cmd, never) {
			t.Errorf("apt-get command contains %q: %s", never, cmd)
		}
	}
	if !strings.Contains(cmd, "--setenv=DEBIAN_FRONTEND=noninteractive") || !strings.Contains(cmd, "--setenv=NEEDRESTART_MODE=a") {
		t.Errorf("env not passed to the transient unit: %s", cmd)
	}
}

// Repair: dpkg --configure -a, then apt-get --fix-broken install, both
// outside the sandbox; a broken state is reported with its own code.
func TestUpdatesRepair(t *testing.T) {
	d := devDeps(t)
	inner := d.Runner.(*system.FakeRunner).Handle
	broken := true
	d.Runner = &system.FakeRunner{Handle: func(c system.Cmd) (system.Result, error) {
		apt := c.Bin == aptGetBin || (c.Bin == systemdRunBin && slices.Contains(c.Args, aptGetBin)) // the plan runs outside the sandbox
		if broken && apt && slices.Contains(c.Args, "upgrade") {
			return system.Result{Stdout: []byte("E: Unmet dependencies. Try 'apt --fix-broken install' with no packages (or specify a solution).\n")},
				&system.ExitError{Bin: c.Bin, ExitCode: 100}
		}
		if apt && slices.Contains(c.Args, "--fix-broken") {
			return system.Result{Stdout: []byte("Inst linux-image-6.8.0-151-generic (6.8.0-151.151 Ubuntu:24.04/noble-updates [amd64])\n")}, nil
		}
		return inner(c)
	}}
	u := newUpdates(d)
	_, err := u.simulate(ctx, UpgradeParams{})
	if !agent.IsCode(err, aptBroken) {
		t.Fatalf("broken state: %v", err)
	}
	plan, err := u.simulate(ctx, UpgradeParams{Fix: true})
	if err != nil || len(plan.NewPackages) != 1 {
		t.Fatalf("repair plan: %+v %v", plan, err)
	}
	if _, err := u.updatesApply(ctx, agent.Actor{}, UpgradeApplyParams{Fix: true, PlanHash: plan.Hash}); err != nil {
		t.Fatal(err)
	}
	var configure, fix bool
	for _, c := range d.Runner.(*system.FakeRunner).CallList() {
		configure = configure || strings.HasPrefix(c, systemdRunBin) && strings.Contains(c, "-- "+dpkgBin+" --configure -a")
		fix = fix || strings.HasPrefix(c, systemdRunBin) && strings.Contains(c, aptGetBin) && strings.Contains(c, "--fix-broken install")
	}
	if !configure || !fix {
		t.Errorf("repair steps: configure=%v fix=%v", configure, fix)
	}
	if (UpgradeParams{Fix: true, Packages: []string{"x"}}).Validate() == nil {
		t.Error("repair with packages accepted")
	}
}

func TestUpdatesApplyRefusesRemovals(t *testing.T) {
	d := devDeps(t)
	d.Runner = &system.FakeRunner{Handle: func(c system.Cmd) (system.Result, error) {
		return system.Result{Stdout: []byte("Inst foo [1] (2 Ubuntu:24.04/noble-updates [amd64])\nRemv oldlib [1]\n")}, nil
	}}
	u := newUpdates(d)
	plan, _ := u.simulate(ctx, UpgradeParams{})
	if len(plan.Removals) != 1 {
		t.Fatalf("plan: %+v", plan)
	}
	if _, err := u.updatesApply(ctx, agent.Actor{}, UpgradeApplyParams{PlanHash: plan.Hash}); err == nil || !strings.Contains(err.Error(), "never removes packages") {
		t.Errorf("got %v", err)
	}
	for _, c := range d.Runner.(*system.FakeRunner).CallList() {
		if strings.Contains(c, aptGetBin) && !strings.Contains(c, "--simulate") {
			t.Errorf("apt-get ran for real: %s", c)
		}
	}
}

// A site that answers differently after the upgrade is reported (apt
// changes can't be rolled back, so the admin needs to know).
func TestUpdatesApplyReportsSites(t *testing.T) {
	d := devDeps(t)
	n := 0
	d.SiteCheck = func(_ context.Context, domain string, _ bool) (int, error) {
		n++
		if domain == "shop.example.org" && n > 4 {
			return 502, nil
		}
		return 200, nil
	}
	u := newUpdates(d)
	plan, _ := u.simulate(ctx, UpgradeParams{})
	out, err := u.updatesApply(ctx, agent.Actor{}, UpgradeApplyParams{PlanHash: plan.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if r := out.(UpgradeResult); r.SitesChanged != 1 {
		t.Errorf("sites: %+v", r.Sites)
	}
}

func TestParseInst(t *testing.T) {
	cases := []struct {
		line string
		want PlannedPkg
	}{
		{"Inst libgd3 [2.3.3-9ubuntu5] (2.3.3-13+ubuntu24.04.1+deb.sury.org+1 PPA for PHP (deprecated):24.04/noble [amd64])",
			PlannedPkg{Name: "libgd3", From: "2.3.3-9ubuntu5", To: "2.3.3-13+ubuntu24.04.1+deb.sury.org+1", Origin: "PPA for PHP (deprecated):24.04/noble"}},
		{"Inst openssl [3.0.13-0ubuntu3.4] (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [amd64])",
			PlannedPkg{Name: "openssl", From: "3.0.13-0ubuntu3.4", To: "3.0.13-0ubuntu3.5", Origin: "Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security", Security: true}},
		{"Inst libheif1 (1.17.6-1ubuntu4 Ubuntu:24.04/noble [amd64])",
			PlannedPkg{Name: "libheif1", To: "1.17.6-1ubuntu4", Origin: "Ubuntu:24.04/noble"}},
		{"Inst nginx-common [1.24.0-2ubuntu7] (1.24.0-2ubuntu7.1 Ubuntu:24.04/noble-updates [all])",
			PlannedPkg{Name: "nginx-common", From: "1.24.0-2ubuntu7", To: "1.24.0-2ubuntu7.1", Origin: "Ubuntu:24.04/noble-updates"}},
	}
	for _, c := range cases {
		got, ok := parseInst(c.line)
		if !ok || got != c.want {
			t.Errorf("parseInst(%q)\n got  %+v\n want %+v", c.line, got, c.want)
		}
	}
	for _, bad := range []string{"Inst", "Inst x", "Inst x [1]", "Conf x (1 y)", "Inst x [1] 2 y"} {
		if _, ok := parseInst(bad); ok {
			t.Errorf("parsed %q", bad)
		}
	}
}

// Ubuntu's phased rollout: apt defers some upgrades; the plan lists them
// apart, and pending doesn't count them as ready.
func TestPhasedUpdates(t *testing.T) {
	out := `Reading package lists...
The following upgrades have been deferred due to phasing:
  libsqlite3-0 libfoo1
The following packages have been kept back:
  libgd3
0 upgraded, 0 newly installed, 0 to remove and 3 not upgraded.
`
	plan := parseSimulation(out)
	if !slices.Equal(plan.Phased, []string{"libsqlite3-0", "libfoo1"}) || !slices.Equal(plan.KeptBack, []string{"libgd3"}) || len(plan.Upgrades) != 0 {
		t.Errorf("plan: %+v", plan)
	}

	up := Updates{Total: 3, Security: 1, Packages: []Package{{Name: "libsqlite3-0"}, {Name: "libgd3"}, {Name: "openssl", Security: true}}}
	markPhased(&up, plan.Phased)
	if up.Total != 2 || up.Phased != 1 || up.Security != 1 || !up.Packages[0].Phased || up.Packages[1].Phased {
		t.Errorf("updates: %+v", up)
	}
}

// The plan runs outside the agent's sandbox (where apt honours phasing),
// like the install itself.
func TestPlanRunsOutsideSandbox(t *testing.T) {
	d := devDeps(t)
	u := newUpdates(d)
	if _, err := u.simulate(ctx, UpgradeParams{}); err != nil {
		t.Fatal(err)
	}
	calls := d.Runner.(*system.FakeRunner).CallList()
	if len(calls) == 0 || !strings.HasPrefix(calls[len(calls)-1], systemdRunBin+" ") {
		t.Errorf("plan not run through systemd-run: %v", calls)
	}
}
