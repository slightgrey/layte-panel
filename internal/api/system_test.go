package api

import "testing"

func TestSystemBreakdowns(t *testing.T) {
	_, c, _ := siteEnv(t)

	st, body := c.do("GET", "/api/v1/system/processes", nil)
	if st != 200 || len(body["processes"].([]any)) == 0 || len(body["groups"].([]any)) == 0 {
		t.Fatalf("processes: %d %v", st, body)
	}
	if mem := body["memory"].(map[string]any); mem["total"].(float64) == 0 {
		t.Errorf("memory: %v", mem)
	}

	st, body = c.do("GET", "/api/v1/system/disk?path=/var", nil)
	if st != 200 || body["path"] != "/var" || len(body["entries"].([]any)) == 0 {
		t.Fatalf("disk: %d %v", st, body)
	}
	if st, body := c.do("GET", "/api/v1/system/disk?path=/var/www&mode=files", nil); st != 200 || len(body["entries"].([]any)) != 2 {
		t.Errorf("large files: %d %v", st, body)
	}
	for _, bad := range []string{"var", "/var/../etc", "/var/"} {
		if st, _ := c.do("GET", "/api/v1/system/disk?path="+bad, nil); st != 400 {
			t.Errorf("path %q: %d", bad, st)
		}
	}
	if st, _ := c.do("GET", "/api/v1/system/disk?path=/no/such", nil); st != 404 {
		t.Errorf("missing folder: %d", st)
	}
}
