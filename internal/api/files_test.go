package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func filesSite(t *testing.T) (*testEnv, *client, string) {
	t.Helper()
	ag, _ := devAgent(t)
	env := newTestEnvWithAgent(t, ag)
	ag.Auditor = storeAudit{env.st}
	c := env.loggedIn(t)
	_, b := c.do("POST", "/api/v1/sites", map[string]any{"domain": "files.test", "php_version": "8.3"})
	waitJob(t, c, b["job_id"].(string))
	return env, c, "/api/v1/sites/" + strconv.Itoa(int(b["site_id"].(float64))) + "/files"
}

func (c *client) upload(t *testing.T, endpoint string, files map[string][]byte) (int, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for name, data := range files {
		fw, _ := mw.CreateFormFile("files", name)
		fw.Write(data)
	}
	mw.Close()
	req, _ := http.NewRequest("POST", c.env.srv.URL+endpoint, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set(csrfHeader, c.csrf)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestFileManager(t *testing.T) {
	env, c, base := filesSite(t)

	st, body := c.do("GET", base+"?path=public", nil)
	if st != 200 || body["read_only"] != false || len(body["entries"].([]any)) != 1 {
		t.Fatalf("list: %d %v", st, body)
	}

	// Editor round trip.
	if st, b := c.do("PUT", base+"/content?path=public/index.php", map[string]string{"content": "<?php echo 'hi';\n"}); st != 200 {
		t.Fatalf("save: %d %v", st, b)
	}
	st, body = c.do("GET", base+"/content?path=public/index.php", nil)
	if st != 200 || body["content"] != "<?php echo 'hi';\n" {
		t.Errorf("read: %d %v", st, body)
	}

	// Upload: one small file and one bigger than a chunk.
	big := bytes.Repeat([]byte("0123456789abcdef"), 70_000) // ~1.1 MB → 3 chunks
	if st, b := c.upload(t, base+"/upload?path=public/uploads", map[string][]byte{"a.txt": []byte("A"), "big.bin": big}); st != 200 {
		// The folder doesn't exist yet.
		if st != 400 {
			t.Fatalf("upload to missing dir: %d %s", st, b)
		}
	}
	c.do("POST", base+"/mkdir", map[string]string{"path": "public/uploads"})
	if st, b := c.upload(t, base+"/upload?path=public/uploads", map[string][]byte{"a.txt": []byte("A"), "big.bin": big}); st != 200 || !strings.Contains(b, "big.bin") {
		t.Fatalf("upload: %d %s", st, b)
	}
	req, _ := http.NewRequest("GET", env.srv.URL+base+"/download?path="+url.QueryEscape("public/uploads/big.bin"), nil)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, big) || !strings.Contains(resp.Header.Get("Content-Disposition"), "big.bin") {
		t.Errorf("download: %d bytes, disposition %q", len(got), resp.Header.Get("Content-Disposition"))
	}

	// Rename + delete.
	if st, _ := c.do("POST", base+"/rename", map[string]string{"path": "public/uploads/a.txt", "to": "public/uploads/b.txt"}); st != 204 {
		t.Errorf("rename: %d", st)
	}
	if st, _ := c.do("POST", base+"/delete", map[string]string{"path": "public/uploads"}); st != 204 {
		t.Errorf("delete: %d", st)
	}

	// Escapes are refused.
	for _, p := range []string{"../..", "../../../etc", "public/../../x"} {
		if st, _ := c.do("GET", base+"?path="+url.QueryEscape(p), nil); st != 400 {
			t.Errorf("list %q: %d", p, st)
		}
	}
	if st, _ := c.do("POST", base+"/delete", map[string]string{"path": ""}); st != 400 {
		t.Errorf("delete site root: %d", st)
	}

	// One audit row per uploaded file (first chunk), no file data in it.
	var n int
	env.st.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='fs.write' AND result='ok' AND args_json LIKE '%big.bin%'`).Scan(&n)
	if n != 1 {
		t.Errorf("audit rows for big.bin = %d, want 1", n)
	}
	var maxLen int
	env.st.DB.QueryRow(`SELECT MAX(length(args_json)) FROM audit_log WHERE action='fs.write'`).Scan(&maxLen)
	if maxLen > 2000 {
		t.Errorf("file data stored in the audit log (%d bytes)", maxLen)
	}
}

func TestFileManagerImportedIsReadOnly(t *testing.T) {
	_, c, _ := siteEnv(t)
	_, scan := c.do("POST", "/api/v1/sites/scan", nil)
	var blog string
	for _, x := range scan["sites"].([]any) {
		s := x.(map[string]any)
		if s["domain"] == "blog.example.com" {
			blog = "/api/v1/sites/" + strconv.Itoa(int(s["id"].(float64))) + "/files"
		}
	}
	st, body := c.do("GET", blog, nil)
	if st != 200 || body["read_only"] != true || body["root"] != "/var/www/blog" {
		t.Fatalf("list imported: %d %v", st, body)
	}
	if st, _ := c.do("GET", blog+"/content?path=wp-config.php", nil); st != 200 {
		t.Errorf("read imported file: %d", st)
	}
	for _, try := range []func() int{
		func() int { st, _ := c.do("PUT", blog+"/content?path=x.php", map[string]string{"content": "x"}); return st },
		func() int { st, _ := c.do("POST", blog+"/delete", map[string]string{"path": "wp-config.php"}); return st },
		func() int { st, _ := c.upload(t, blog+"/upload", map[string][]byte{"x.php": []byte("x")}); return st },
	} {
		if st := try(); st != 409 {
			t.Errorf("write on imported site: %d", st)
		}
	}
}
