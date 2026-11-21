package api

import (
	"strconv"
	"testing"

	"github.com/slightgrey/layte-panel/internal/actions"
)

type securityView struct {
	rules map[string]map[string]any // by "to from"
	body  map[string]any
}

func getSecurity(t *testing.T, c *client) securityView {
	t.Helper()
	st, body := c.do("GET", "/api/v1/security", nil)
	if st != 200 {
		t.Fatalf("security: %d %v", st, body)
	}
	v := securityView{rules: map[string]map[string]any{}, body: body}
	ufw := body["overview"].(map[string]any)["ufw"].(map[string]any)
	for _, x := range ufw["rules"].([]any) {
		r := x.(map[string]any)
		v.rules[r["to"].(string)+" "+r["from"].(string)] = r
	}
	return v
}

func TestSecurityOverview(t *testing.T) {
	_, c, _ := siteEnv(t)
	v := getSecurity(t, c)
	ov := v.body["overview"].(map[string]any)

	if ov["ufw"].(map[string]any)["active"] != true || len(v.rules) != 3 {
		t.Errorf("ufw: %v", ov["ufw"])
	}
	if v.rules["22/tcp Anywhere"]["locked"] != true || v.rules["80,443/tcp Anywhere"]["locked"] != true || v.rules["3306/tcp 10.0.0.0/24"]["locked"] != false {
		t.Errorf("locked flags: %v", v.rules)
	}
	logins := ov["failed_logins"].([]any)
	top := logins[0].(map[string]any)
	if ov["failed_total"] != float64(6) || top["ip"] != "203.0.113.50" || top["count"] != float64(4) {
		t.Errorf("failed logins: total=%v top=%v", ov["failed_total"], top)
	}
	if v.body["updates"].(map[string]any)["total"] != float64(5) {
		t.Errorf("updates: %v", v.body["updates"])
	}
}

func TestUFWRules(t *testing.T) {
	_, c, _ := siteEnv(t)

	if st, b := c.do("POST", "/api/v1/security/ufw/rules", map[string]string{"port": "8080", "proto": "tcp", "from": "203.0.113.0/24"}); st != 204 {
		t.Fatalf("allow: %d %v", st, b)
	}
	for _, bad := range []map[string]string{
		{"port": "99999"},
		{"port": "22; rm -rf /"},
		{"port": "8000:8100"}, // range without protocol
		{"port": "80", "from": "evil.example.com"},
		{"port": "80", "proto": "icmp"},
	} {
		if st, _ := c.do("POST", "/api/v1/security/ufw/rules", bad); st != 400 {
			t.Errorf("%v: %d", bad, st)
		}
	}

	v := getSecurity(t, c)
	rule := v.rules["8080/tcp 203.0.113.0/24"]
	if rule == nil {
		t.Fatalf("new rule missing: %v", v.rules)
	}
	num := strconv.Itoa(int(rule["num"].(float64)))

	// Stale text (rules changed) → refused; SSH rule → refused; correct → deleted.
	if st, _ := c.do("DELETE", "/api/v1/security/ufw/rules/"+num, map[string]string{"expect": "something else"}); st != 400 {
		t.Errorf("stale delete: %d", st)
	}
	ssh := v.rules["22/tcp Anywhere"]
	if st, b := c.do("DELETE", "/api/v1/security/ufw/rules/"+strconv.Itoa(int(ssh["num"].(float64))), map[string]string{"expect": ssh["text"].(string)}); st != 400 {
		t.Errorf("deleting SSH rule: %d %v", st, b)
	}
	if st, b := c.do("DELETE", "/api/v1/security/ufw/rules/"+num, map[string]string{"expect": rule["text"].(string)}); st != 204 {
		t.Errorf("delete: %d %v", st, b)
	}
	if _, ok := getSecurity(t, c).rules["8080/tcp 203.0.113.0/24"]; ok {
		t.Error("rule still there")
	}

	// Disable, then enable re-adds SSH/80/443 first.
	c.do("POST", "/api/v1/security/ufw/enabled", map[string]bool{"enable": false})
	if getSecurity(t, c).body["overview"].(map[string]any)["ufw"].(map[string]any)["active"] != false {
		t.Error("not disabled")
	}
	if st, _ := c.do("POST", "/api/v1/security/ufw/enabled", map[string]bool{"enable": true}); st != 204 {
		t.Error("enable failed")
	}
	v = getSecurity(t, c)
	for _, want := range []string{"22/tcp Anywhere", "80/tcp Anywhere", "443/tcp Anywhere"} {
		if v.rules[want] == nil {
			t.Errorf("%s not allowed after enable: %v", want, v.rules)
		}
	}
}

func TestInstallUpdatesJob(t *testing.T) {
	_, c, _ := siteEnv(t)
	st, body := c.do("POST", "/api/v1/security/updates", nil)
	if st != 202 {
		t.Fatalf("%d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Errorf("job: %v", job)
	}
}

func TestProtectsRule(t *testing.T) {
	ssh := []string{"2222"}
	tests := []struct {
		r    actions.UFWRule
		want bool
	}{
		{actions.UFWRule{To: "2222/tcp", Action: "ALLOW IN", From: "Anywhere"}, true},
		{actions.UFWRule{To: "OpenSSH", Action: "ALLOW IN", From: "Anywhere"}, true},
		{actions.UFWRule{To: "443", Action: "ALLOW IN", From: "Anywhere (v6)"}, true},
		{actions.UFWRule{To: "22/tcp", Action: "ALLOW IN", From: "Anywhere"}, false}, // sshd moved to 2222
		{actions.UFWRule{To: "2222/tcp", Action: "ALLOW IN", From: "10.0.0.1"}, false},
		{actions.UFWRule{To: "2222/tcp", Action: "DENY IN", From: "Anywhere"}, false},
	}
	for _, tt := range tests {
		if got := actions.Protects(tt.r, ssh); got != tt.want {
			t.Errorf("%+v: %v, want %v", tt.r, got, tt.want)
		}
	}
}
