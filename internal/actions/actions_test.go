package actions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/system"
)

var ctx = context.Background()

func TestSystemInfo(t *testing.T) {
	out, err := DevDeps().systemInfo(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		t.Fatal(err)
	}
	info := out.(SystemInfo)
	if info.Hostname != "vps-demo-01" || info.OS != "Ubuntu 24.04.1 LTS" || info.OSVersionID != "24.04" {
		t.Errorf("identity: %+v", info)
	}
	if info.CPUs != 2 || info.UptimeSeconds != 1234567 || info.Load != [3]float64{0.12, 0.08, 0.05} {
		t.Errorf("cpu/uptime/load: %+v", info)
	}
	if info.Memory.Total != 2014256*1024 || info.Memory.Used != (2014256-1203456)*1024 || info.Memory.SwapUsed != (1048572-996000)*1024 {
		t.Errorf("memory: %+v", info.Memory)
	}
	// "/" and "/var" are the same filesystem in the fixture, so only 2 disks.
	if len(info.Disks) != 2 || info.Disks[0].Path != "/" || info.Disks[1].Path != "/home" {
		t.Errorf("disks: %+v", info.Disks)
	}
}

func TestServicesStatus(t *testing.T) {
	d := DevDeps()
	out, err := d.servicesStatus(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		t.Fatal(err)
	}
	svcs := out.([]Service)
	byName := map[string]Service{}
	for _, s := range svcs {
		byName[s.Name] = s
	}
	if s := byName["nginx"]; !s.Installed || s.Active != "active" || s.Sub != "running" {
		t.Errorf("nginx: %+v", s)
	}
	if s := byName["mariadb"]; s.Installed {
		t.Errorf("mariadb should be not installed: %+v", s)
	}
	if s := byName["php8.2-fpm"]; s.Active != "failed" {
		t.Errorf("php8.2-fpm: %+v", s)
	}
	// The PHP units discovered from /etc/php must be passed to systemctl.
	calls := d.Runner.(*system.FakeRunner).Calls
	if len(calls) != 1 || !strings.Contains(calls[0], "php8.2-fpm.service") || !strings.Contains(calls[0], "php8.3-fpm.service") {
		t.Errorf("systemctl call: %v", calls)
	}
}

func TestParseSystemctlDedup(t *testing.T) {
	out := "Id=mariadb.service\nLoadState=loaded\n\nId=mariadb.service\nLoadState=loaded\n"
	if got := parseSystemctlShow(out); len(got) != 1 {
		t.Errorf("got %d services, want 1", len(got))
	}
}

func TestVersions(t *testing.T) {
	out, err := DevDeps().versions(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Version{}
	for _, v := range out.([]Version) {
		got[v.Name] = v
	}
	want := map[string]string{"nginx": "1.24.0", "php8.2": "8.2.24", "php8.3": "8.3.6", "mysql": "8.0.39", "certbot": "2.9.0"}
	for name, ver := range want {
		if g := got[name]; !g.Installed || g.Version != ver {
			t.Errorf("%s = %+v, want %s", name, g, ver)
		}
	}
}

func TestVersionsNotInstalled(t *testing.T) {
	d := DevDeps()
	d.Runner = &system.FakeRunner{} // nothing installed
	out, err := d.versions(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range out.([]Version) {
		if v.Installed {
			t.Errorf("%s reported installed", v.Name)
		}
	}
}

func TestParseMySQLVersion(t *testing.T) {
	tests := []struct{ in, name, ver string }{
		{"mysql  Ver 8.0.39-0ubuntu0.24.04.2 for Linux on x86_64 ((Ubuntu))", "mysql", "8.0.39"},
		{"mysql  Ver 15.1 Distrib 10.11.8-MariaDB, for debian-linux-gnu (x86_64)", "mariadb", "10.11.8"},
		{"/usr/bin/mysql from 11.4.2-MariaDB, client 15.2 for debian-linux-gnu (x86_64)", "mariadb", "11.4.2"},
	}
	for _, tt := range tests {
		n, v := parseMySQLVersion(tt.in)
		if n != tt.name || v != tt.ver {
			t.Errorf("%q → %s %s, want %s %s", tt.in, n, v, tt.name, tt.ver)
		}
	}
}

func TestUpdatesPendingCached(t *testing.T) {
	d := DevDeps()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d.Now = func() time.Time { return now }
	u := &updates{deps: d}

	out, err := u.pending(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		t.Fatal(err)
	}
	up := out.(Updates)
	if up.Total != 5 || up.Security != 2 || up.Packages[0].Name != "libssl3t64" || up.Packages[0].From != "3.0.13-0ubuntu3.1" {
		t.Errorf("updates: %+v", up)
	}

	u.pending(ctx, agent.Actor{}, agent.NoParams{}) // cached
	now = now.Add(updatesTTL + time.Second)
	u.pending(ctx, agent.Actor{}, agent.NoParams{}) // expired → runs again
	if n := len(d.Runner.(*system.FakeRunner).Calls); n != 2 {
		t.Errorf("apt ran %d times, want 2", n)
	}
}

func TestRegister(t *testing.T) {
	reg := agent.NewRegistry()
	Register(reg, DevDeps())
	for _, name := range []string{"system.info", "services.status", "versions.get", "updates.pending"} {
		a, ok := reg.Lookup(name)
		if !ok || !a.ReadOnly {
			t.Errorf("%s: registered=%v readOnly=%v", name, ok, a.ReadOnly)
		}
	}
}
