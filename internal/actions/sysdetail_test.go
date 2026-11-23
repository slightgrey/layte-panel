package actions

import (
	"strings"
	"testing"
	"time"

	"github.com/slightgrey/layte-panel/internal/agent"
)

func TestMaskSecrets(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/bin/mysql -uroot -pSecret1 shop":          "/usr/bin/mysql -uroot -p*** shop",
		"mysqldump --password=abc db":                   "mysqldump --password=*** db",
		"app --token=xyz --pid-file=/run/a.pid":         "app --token=*** --pid-file=/run/a.pid",
		"curl https://x/?password=hunter2":              "curl https://x/?password=***",
		"nginx: worker process":                         "nginx: worker process",
		"/usr/sbin/php-fpm8.3 --nodaemonize -p /prefix": "/usr/sbin/php-fpm8.3 --nodaemonize -p /prefix", // -p is not a password here
	} {
		if got := maskSecrets(in); got != want {
			t.Errorf("maskSecrets(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProcGroup(t *testing.T) {
	cases := []struct{ name, cmd, cgroup, want string }{
		{"php-fpm8.3", "php-fpm: pool blog.example.com", "0::/system.slice/php8.3-fpm.service", "PHP pool blog.example.com"},
		{"php-fpm8.3", "php-fpm: master process (/etc/php/8.3/fpm/php-fpm.conf)", "0::/system.slice/php8.3-fpm.service", "PHP-FPM master"},
		{"mysqld", "/usr/sbin/mysqld", "0::/system.slice/mysql.service", "mysql"},
		{"bash", "-bash", "0::/user.slice/user-1000.slice/session-3.scope", "SSH/login sessions"},
		{"thing", "thing", "", "thing"},
		{"systemd", "/sbin/init", "0::/init.scope", "systemd"},
	}
	for _, c := range cases {
		if got := procGroup(c.name, c.cmd, c.cgroup); got != c.want {
			t.Errorf("procGroup(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestSystemProcesses(t *testing.T) {
	cpuSample = time.Millisecond
	d := devDeps(t)
	out, err := d.systemProcesses(ctx, agent.Actor{}, agent.NoParams{})
	if err != nil {
		t.Fatal(err)
	}
	r := out.(ProcessesResult)
	if len(r.Procs) != 6 || r.CPUs == 0 || r.Memory.Total == 0 || r.Memory.AppsUsed == 0 {
		t.Fatalf("result: %+v", r)
	}
	if r.Procs[0].Name != "mysqld" || r.Procs[0].User != "mysql" || r.Procs[0].RSS != 420000*1024 {
		t.Errorf("largest process: %+v", r.Procs[0])
	}
	groups := map[string]ProcGroup{}
	for _, g := range r.Groups {
		groups[g.Name] = g
	}
	if g := groups["PHP pool shop"]; g.Processes != 1 || g.RSS != 95000*1024 {
		t.Errorf("PHP pool group: %+v (all: %+v)", g, r.Groups)
	}
	for _, p := range r.Procs {
		if strings.Contains(p.Command, "SuperSecret1") {
			t.Errorf("password shown: %q", p.Command)
		}
		if p.CPU < 0 || p.CPU > 100 {
			t.Errorf("cpu out of range: %+v", p)
		}
	}
}

func TestDiskBreakdown(t *testing.T) {
	d := devDeps(t)
	out, err := d.diskBreakdown(ctx, agent.Actor{}, DiskBreakdownParams{Path: "/var"})
	if err != nil {
		t.Fatal(err)
	}
	b := out.(DiskBreakdown)
	if b.Total == 0 || len(b.Entries) == 0 || b.Entries[0].Bytes < b.Entries[len(b.Entries)-1].Bytes {
		t.Errorf("folders: %+v", b)
	}
	for _, e := range b.Entries {
		if !strings.HasPrefix(e.Path, "/var/") {
			t.Errorf("entry outside /var: %s", e.Path)
		}
	}
	out, err = d.diskBreakdown(ctx, agent.Actor{}, DiskBreakdownParams{Path: "/var/www", Mode: "files"})
	if err != nil {
		t.Fatal(err)
	}
	if f := out.(DiskBreakdown).Entries; len(f) != 2 || f[0].Path != "/var/www/backup-old.tar.gz" {
		t.Errorf("files: %+v", f)
	}
	for _, bad := range []DiskBreakdownParams{{Path: "var"}, {Path: "/var/../etc"}, {Path: "/var/"}, {Path: "/", Mode: "x"}} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if _, err := d.diskBreakdown(ctx, agent.Actor{}, DiskBreakdownParams{Path: "/no/such/dir"}); err == nil {
		t.Error("missing folder accepted")
	}
}
