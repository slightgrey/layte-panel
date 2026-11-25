package api

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

const sftpTestKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGhV5zFqYBsCm/yHg5lX2yNSv3SrUdKjSRaXzt1TZdQt vince@pc"

func TestSFTP(t *testing.T) {
	_, c, deps := siteEnv(t)
	id := strconv.Itoa(createSite(t, c, "sftp.test", "8.3"))
	path := "/api/v1/sites/" + id + "/sftp"
	user := "sftptest"

	_, st := c.do("GET", path, nil)
	if st["enabled"] != false || st["username"] != user || st["folder"] != "/sftp.test" {
		t.Fatalf("initial: %v", st)
	}
	for _, bad := range []map[string]any{
		{"enabled": true}, // no key, no password
		{"enabled": true, "keys": []string{"not a key"}}, // bad key
		{"enabled": true, "keys": []string{"command=\"x\" " + sftpTestKey}},
	} {
		if code, body := c.do("PUT", path, bad); code != 400 {
			t.Errorf("%v: %d %v", bad, code, body)
		}
	}

	// On, with a key (pasted twice: stored once).
	code, st := c.do("PUT", path, map[string]any{"enabled": true, "keys": []string{sftpTestKey, "  " + sftpTestKey + " "}})
	if code != 200 || st["enabled"] != true || len(st["keys"].([]any)) != 1 {
		t.Fatalf("enable: %d %v", code, st)
	}
	if k := st["keys"].([]any)[0].(map[string]any); !strings.HasPrefix(k["fingerprint"].(string), "SHA256:") {
		t.Errorf("key: %v", k)
	}
	if b, _ := deps.FS.ReadFile("/etc/ssh/layte-sftp/" + user); string(b) != sftpTestKey+"\n" {
		t.Errorf("keys file: %q", b)
	}
	if ok, _ := deps.FS.Exists("/etc/ssh/sshd_config.d/zz-layte-sftp.conf"); !ok {
		t.Error("sshd drop-in missing")
	}
	// No password option yet: no password.
	if code, _ := c.do("POST", path+"/password", nil); code != 400 {
		t.Errorf("password while off: %d", code)
	}

	// Password logins on: a password, shown once, never stored.
	if code, st = c.do("PUT", path, map[string]any{"enabled": true, "password": true, "keys": []string{sftpTestKey}}); code != 200 || st["password"] != true {
		t.Fatalf("password on: %d %v", code, st)
	}
	code, body := c.do("POST", path+"/password", nil)
	pw, _ := body["password"].(string)
	if code != 200 || len(pw) != 24 {
		t.Fatalf("password: %d %v", code, body)
	}
	if _, site := c.do("GET", "/api/v1/sites/"+id, nil); strings.Contains(toJSONString(site), pw) {
		t.Error("password stored with the site")
	}

	// Off: keys kept in Layte for later, gone from the server.
	if code, st = c.do("PUT", path, map[string]any{"enabled": false, "keys": []string{sftpTestKey}}); code != 200 || st["enabled"] != false || st["password"] != false {
		t.Fatalf("disable: %d %v", code, st)
	}
	if ok, _ := deps.FS.Exists("/etc/ssh/layte-sftp/" + user); ok {
		t.Error("keys file left after turning SFTP off")
	}

	// Deleting the site turns SFTP off first.
	c.do("PUT", path, map[string]any{"enabled": true, "keys": []string{sftpTestKey}})
	_, body = c.do("DELETE", "/api/v1/sites/"+id, map[string]any{"confirm_domain": "sftp.test"})
	if job := waitJob(t, c, body["job_id"].(string)); job["status"] != "succeeded" {
		t.Fatalf("delete: %v", job)
	}
	if ok, _ := deps.FS.Exists("/etc/systemd/system/layte-sftp-" + user + ".service"); ok {
		t.Error("mount unit left after deleting the site")
	}
}

func toJSONString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
