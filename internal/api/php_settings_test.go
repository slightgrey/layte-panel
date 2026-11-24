package api

import (
	"strconv"
	"strings"
	"testing"
)

func TestPHPSettings(t *testing.T) {
	_, c, deps := siteEnv(t)
	id := createSite(t, c, "ini.test", "8.3")
	path := "/api/v1/sites/" + strconv.Itoa(id) + "/php-settings"
	pool := "/etc/php/8.3/fpm/pool.d/layte-ini.test.conf"
	vhostPath := "/etc/nginx/sites-available/layte-ini.test.conf"

	for _, bad := range []map[string]any{
		{"memory_limit": "lots"},
		{"upload_max": "64M\nphp_admin_value[open_basedir] = /"},
		{"max_execution_time": 99999},
		{},
	} {
		if st, body := c.do("PUT", path, bad); st != 400 {
			t.Errorf("%v: %d %v", bad, st, body)
		}
	}

	st, body := c.do("PUT", path, map[string]any{"memory_limit": "512m", "upload_max": "256M", "max_execution_time": 600, "max_input_vars": 5000})
	if st != 202 {
		t.Fatalf("set: %d %v", st, body)
	}
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("job: %v", job)
	}
	p, _ := deps.FS.ReadFile(pool)
	for _, want := range []string{"memory_limit] = 512M", "upload_max_filesize] = 256M", "post_max_size] = 256M", "max_execution_time] = 600", "max_input_vars] = 5000"} {
		if !strings.Contains(string(p), want) {
			t.Errorf("pool lacks %q:\n%s", want, p)
		}
	}
	v, _ := deps.FS.ReadFile(vhostPath)
	if !strings.Contains(string(v), "client_max_body_size 256m;") || !strings.Contains(string(v), "fastcgi_read_timeout 630;") {
		t.Errorf("vhost:\n%s", v)
	}
	_, site := c.do("GET", "/api/v1/sites/"+strconv.Itoa(id), nil)
	if ps, _ := site["php_settings"].(map[string]any); ps["memory_limit"] != "512M" || ps["max_input_vars"] != float64(5000) {
		t.Errorf("stored: %v", site["php_settings"])
	}

	// A PHP switch keeps the settings.
	st, body = c.do("POST", "/api/v1/sites/"+strconv.Itoa(id)+"/php", map[string]string{"version": "8.2"})
	if st != 202 {
		t.Fatalf("switch: %d %v", st, body)
	}
	waitJob(t, c, body["job_id"].(string))
	if p, _ := deps.FS.ReadFile("/etc/php/8.2/fpm/pool.d/layte-ini.test.conf"); !strings.Contains(string(p), "memory_limit] = 512M") {
		t.Errorf("settings lost on PHP switch:\n%s", p)
	}
}
