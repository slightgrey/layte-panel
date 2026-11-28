package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/system"
)

// Disk quotas: off until an admin turns them on; then a login's disk limit
// becomes a hard quota for its Linux user.
func TestDiskQuotas(t *testing.T) {
	_, admin, deps := siteEnv(t)
	mainID := createSite(t, admin, "quota.test", "8.3")

	if code, body := admin.do("GET", "/api/v1/quota", nil); code != 200 || body["enabled"] != false || body["supported"] != true {
		t.Fatalf("status: %d %v", code, body)
	}
	code, body := admin.do("POST", "/api/v1/users", map[string]any{"username": "quotaco", "site_id": mainID})
	if code != 201 {
		t.Fatalf("create login: %d %v", code, body)
	}
	path := fmt.Sprintf("/api/v1/users/%d", int(body["user"].(map[string]any)["id"].(float64)))
	// A disk limit needs quotas on.
	if code, body := admin.do("PATCH", path, map[string]any{"disk_mb": 2048}); code != 409 || errCode(body) != "quota_off" {
		t.Errorf("disk limit without quotas: %d %v", code, body)
	}

	code, body = admin.do("POST", "/api/v1/quota/enable", nil)
	if code != 200 || body["enabled"] != true {
		t.Fatalf("enable: %d %v", code, body)
	}
	if b, _ := deps.FS.ReadFile("/etc/fstab"); !strings.Contains(string(b), "discard,errors=remount-ro,usrquota") || !strings.Contains(string(b), "/boot/efi") {
		t.Errorf("fstab:\n%s", b)
	}
	if code, body := admin.do("PATCH", path, map[string]any{"disk_mb": 2048}); code != 200 {
		t.Fatalf("set disk limit: %d %v", code, body)
	}
	if b, _ := deps.FS.ReadFile("/var/lib/layte-dev/quota/quotatest"); string(b) != "2097152" { // hard limit in KB
		t.Errorf("quota hard limit = %q", b)
	}

	// Usage: the slice's cgroup (made up here) and the quota.
	cred, _ := deps.Users.Lookup("quotatest")
	cg := "/sys/fs/cgroup/layte.slice/layte-acct.slice/" + actions.AccountSlice(cred.UID)
	for f, v := range map[string]string{"cpu.stat": "usage_usec 1000\nuser_usec 900\n", "cpu.max": "50000 100000\n", "memory.current": "104857600\n", "memory.max": "536870912\n", "pids.current": "4\n"} {
		deps.FS.MkdirAll(cg, 0o755)
		deps.FS.WriteFileAtomic(cg+"/"+f, []byte(v), 0o644)
	}
	code, body = admin.do("GET", path+"/usage", nil)
	if code != 200 || body["active"] != true || body["cpu_limit"] != float64(50) || body["memory_bytes"] != float64(104857600) ||
		body["memory_limit"] != float64(536870912) || body["processes"] != float64(4) ||
		body["disk_exact"] != true || body["disk_bytes"] != float64(51_200_000/1024*1024) || body["disk_limit"] != float64(2<<30) {
		t.Errorf("usage: %d %v", code, body)
	}

	// Disk use is cached: refreshes reuse it, ?fresh=1 measures again.
	quotaReads := func() int {
		n := 0
		for _, c := range deps.Runner.(*system.FakeRunner).CallList() {
			if strings.Contains(c, "/usr/bin/quota ") {
				n++
			}
		}
		return n
	}
	before := quotaReads()
	if code, body := admin.do("GET", path+"/usage", nil); code != 200 || body["disk_bytes"] != float64(51_200_000/1024*1024) || quotaReads() != before {
		t.Errorf("cached usage: %d %v (quota reads %d → %d)", code, body, before, quotaReads())
	}
	if code, _ := admin.do("GET", path+"/usage?fresh=1", nil); code != 200 || quotaReads() != before+1 {
		t.Errorf("fresh usage didn't measure disk: %d (quota reads %d → %d)", code, before, quotaReads())
	}
}
