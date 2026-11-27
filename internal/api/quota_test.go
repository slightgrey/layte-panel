package api

import (
	"fmt"
	"strings"
	"testing"
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
}
