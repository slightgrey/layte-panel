package api

import (
	"strconv"
	"strings"
	"testing"

	"github.com/slightgrey/layte-panel/internal/validate"
)

func TestCron(t *testing.T) {
	_, c, deps := siteEnv(t)
	_, b := c.do("POST", "/api/v1/sites", map[string]any{"domain": "cron.test", "php_version": "8.3",
		"wordpress": map[string]string{"title": "C", "admin_user": "owner", "admin_email": "o@example.com"}})
	waitJob(t, c, b["job_id"].(string))
	siteID := strconv.Itoa(int(b["site_id"].(float64)))
	const file = "/etc/cron.d/layte-cron_test"

	_, list := c.do("GET", "/api/v1/cron", nil)
	suggestion := list["wp_cron"].(map[string]any)[siteID].(string)
	if !strings.Contains(suggestion, "cron event run --due-now") {
		t.Errorf("wp cron suggestion: %q", suggestion)
	}

	st, body := c.do("POST", "/api/v1/sites/"+siteID+"/cron", map[string]string{"schedule": "*/5  * * * *", "command": suggestion})
	if st != 201 {
		t.Fatalf("add: %d %v", st, body)
	}
	id := strconv.Itoa(int(body["id"].(float64)))
	c.do("POST", "/api/v1/sites/"+siteID+"/cron", map[string]string{"schedule": "@daily", "command": `date +%F >> backup.txt`})

	content, _ := deps.FS.ReadFile(file)
	s := string(content)
	if !strings.HasPrefix(s, "# Managed by Layte Panel") ||
		!strings.Contains(s, "*/5 * * * * crontest cd /home/crontest/cron.test && ( "+suggestion+" ) >> /home/crontest/cron.test/logs/cron.log 2>&1") ||
		!strings.Contains(s, `date +\%F`) || strings.Contains(s, " root ") {
		t.Errorf("cron file:\n%s", s)
	}

	// Pause → removed from the file; resume → back.
	c.do("PATCH", "/api/v1/cron/"+id, map[string]bool{"enabled": false})
	content, _ = deps.FS.ReadFile(file)
	if strings.Contains(string(content), "*/5") {
		t.Error("paused job still in cron file")
	}
	c.do("DELETE", "/api/v1/cron/"+id, nil)
	_, list = c.do("GET", "/api/v1/cron", nil)
	if n := len(list["jobs"].([]any)); n != 1 {
		t.Errorf("jobs = %d", n)
	}

	// Bad input never reaches the file.
	for _, bad := range []map[string]string{
		{"schedule": "* * * *", "command": "x"},
		{"schedule": "* * * * * root", "command": "x"},
		{"schedule": "* * * * *", "command": "a\nb"},
		{"schedule": "@sometimes", "command": "x"},
	} {
		if st, _ := c.do("POST", "/api/v1/sites/"+siteID+"/cron", bad); st != 400 {
			t.Errorf("%v: %d", bad, st)
		}
	}

	// Deleting the site removes its cron file.
	_, d := c.do("DELETE", "/api/v1/sites/"+siteID, map[string]any{"confirm_domain": "cron.test", "remove_files": true, "drop_db": true})
	waitJob(t, c, d["job_id"].(string))
	if exists(t, deps.FS, file) {
		t.Error("cron file left after site delete")
	}
	if !exists(t, deps.FS, "/etc/cron.d/certbot") {
		t.Error("foreign cron file touched")
	}
}

func TestCronValidation(t *testing.T) {
	for _, ok := range []string{"*/5 * * * *", "0 3 * * 1-5", "15,45 * * jan mon", "@hourly"} {
		if err := validate.CronSchedule(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "* * * * * *", "* * * * ;", "@reboot2", "*/5 * * * * root"} {
		if validate.CronSchedule(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
