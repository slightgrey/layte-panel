package api

import (
	"strings"
	"testing"
)

func TestInstallUpdates(t *testing.T) {
	_, c, _ := siteEnv(t)
	c.do("POST", "/api/v1/sites/scan", nil)

	st, plan := c.do("GET", "/api/v1/updates/plan", nil)
	if st != 200 || len(plan["upgrades"].([]any)) != 3 || len(plan["kept_back"].([]any)) != 2 || plan["hash"] == "" {
		t.Fatalf("plan: %d %v", st, plan)
	}
	if st, _ := c.do("GET", "/api/v1/updates/plan?packages=-o", nil); st != 400 {
		t.Errorf("bad package name: %d", st)
	}
	if st, _ := c.do("POST", "/api/v1/updates/apply", map[string]any{}); st != 400 {
		t.Errorf("apply without hash: %d", st)
	}
	// A stale preview fails the job without installing anything.
	_, body := c.do("POST", "/api/v1/updates/apply", map[string]any{"plan_hash": "stale"})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "failed" || !strings.Contains(job["error"].(string), "changed since the preview") {
		t.Errorf("stale plan: %v", job)
	}
	st, body = c.do("POST", "/api/v1/updates/apply", map[string]any{"plan_hash": plan["hash"]})
	if st != 202 {
		t.Fatalf("apply: %d %v", st, body)
	}
	job := waitJob(t, c, body["job_id"].(string))
	if job["status"] != "succeeded" {
		t.Fatalf("apply job: %v", job)
	}
	if res := job["result"].(map[string]any); res["installed"].(float64) != 3 || res["sites_changed"].(float64) != 0 {
		t.Errorf("result: %v", res)
	}

	// Kept-back packages, on request.
	_, kept := c.do("GET", "/api/v1/updates/plan?packages=libgd3,libgd-dev", nil)
	if n := kept["new_packages"].([]any); len(n) != 1 {
		t.Errorf("kept-back plan: %v", kept)
	}
	if st, _ := c.do("POST", "/api/v1/updates/refresh", nil); st != 204 {
		t.Errorf("refresh: %d", st)
	}
}
