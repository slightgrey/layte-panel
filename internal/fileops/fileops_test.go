package fileops

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func jail(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "public", "wp-content"), 0o755)
	os.WriteFile(filepath.Join(dir, "public", "index.php"), []byte("<?php echo 1;\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "public", "logo.png"), []byte{0x89, 'P', 'N', 'G', 0, 0}, 0o644)
	return dir
}

func TestCleanPath(t *testing.T) {
	ok := map[string]string{"": ".", "/": ".", "public": "public", "/public/./y": "public/y", "a//b": "a/b"}
	for in, want := range ok {
		if got, err := CleanPath(in); err != nil || got != want {
			t.Errorf("CleanPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"..", "../etc", "a/../../b", "public/x/../y", "x\x00y"} {
		if _, err := CleanPath(bad); err == nil {
			t.Errorf("CleanPath(%q) accepted", bad)
		}
	}
}

func TestListReadWrite(t *testing.T) {
	root := jail(t)
	r := Do(Request{Op: "list", Root: root, Path: "public"})
	if r.Error != "" || len(r.Entries) != 3 || r.Entries[0].Name != "wp-content" || r.Entries[0].Type != "dir" {
		t.Fatalf("list: %+v", r)
	}
	if r := Do(Request{Op: "read", Root: root, Path: "public/index.php"}); !r.Text || string(r.Data) != "<?php echo 1;\n" {
		t.Errorf("read text: %+v", r)
	}
	if r := Do(Request{Op: "read", Root: root, Path: "public/logo.png"}); r.Text {
		t.Error("binary reported as text")
	}

	// Chunked write: the target only changes on the final chunk.
	Do(Request{Op: "write", Root: root, Path: "public/index.php", Chunk: []byte("<?php echo "), Offset: 0})
	if b, _ := os.ReadFile(filepath.Join(root, "public", "index.php")); string(b) != "<?php echo 1;\n" {
		t.Error("target replaced before the final chunk")
	}
	if l := Do(Request{Op: "list", Root: root, Path: "public"}); len(l.Entries) != 3 {
		t.Errorf("temp upload file listed: %+v", l.Entries)
	}
	r = Do(Request{Op: "write", Root: root, Path: "public/index.php", Chunk: []byte("2;\n"), Offset: 11, Final: true})
	if r.Error != "" || r.Size != 14 {
		t.Fatalf("final write: %+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "public", "index.php")); string(b) != "<?php echo 2;\n" {
		t.Errorf("content = %q", b)
	}
}

func TestMkdirRenameDelete(t *testing.T) {
	root := jail(t)
	if r := Do(Request{Op: "mkdir", Root: root, Path: "public/new/deep"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if r := Do(Request{Op: "rename", Root: root, Path: "public/new", To: "public/renamed"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if r := Do(Request{Op: "rename", Root: root, Path: "public/renamed", To: "public/index.php"}); r.Error == "" {
		t.Error("rename over an existing file allowed")
	}
	if r := Do(Request{Op: "delete", Root: root, Path: "public/renamed"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if r := Do(Request{Op: "delete", Root: root, Path: "/"}); r.Error == "" {
		t.Error("deleted the jail root")
	}
}

func TestJailAndReadOnly(t *testing.T) {
	root := jail(t)
	outside := filepath.Join(filepath.Dir(root), "outside-secret.txt")
	os.WriteFile(outside, []byte("secret"), 0o644)
	t.Cleanup(func() { os.Remove(outside) })

	for _, req := range []Request{
		{Op: "read", Root: root, Path: "../outside-secret.txt"},
		{Op: "rename", Root: root, Path: "public/index.php", To: "../escaped.php"},
		{Op: "write", Root: root, Path: "../x", Chunk: []byte("x"), Final: true},
	} {
		if r := Do(req); r.Error == "" {
			t.Errorf("%s %s escaped the jail", req.Op, req.Path)
		}
	}
	if runtime.GOOS != "windows" {
		os.Symlink(outside, filepath.Join(root, "public", "link.txt"))
		if r := Do(Request{Op: "read", Root: root, Path: "public/link.txt"}); r.Error == "" || strings.Contains(string(r.Data), "secret") {
			t.Errorf("followed a symlink out of the jail: %+v", r)
		}
	}
	for _, op := range []string{"write", "mkdir", "delete", "rename"} {
		if r := Do(Request{Op: op, Root: root, Path: "public/x", To: "public/y", ReadOnly: true}); !strings.Contains(r.Error, "read-only") {
			t.Errorf("%s allowed on a read-only site: %+v", op, r)
		}
	}
}

func TestServe(t *testing.T) {
	root := jail(t)
	var out strings.Builder
	Serve(strings.NewReader(`{"op":"list","root":`+quote(root)+`,"path":"public"}`), &out)
	if !strings.Contains(out.String(), `"name":"index.php"`) {
		t.Errorf("serve: %s", out.String())
	}
	out.Reset()
	Serve(strings.NewReader(`{"op":"list","evil":1}`), &out)
	if !strings.Contains(out.String(), "bad request") {
		t.Errorf("unknown field accepted: %s", out.String())
	}
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }
