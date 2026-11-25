package system

import (
	"context"
	"strings"
	"testing"
)

const adminVhost = "server { server_name a.test; }\n"

func replaceSpec(old, keep string) ReplaceSpec {
	return ReplaceSpec{New: spec(Marker + "\nlayte\n"), Old: old, Keep: keep, Expected: []byte(adminVhost)}
}

func TestReplaceConfig(t *testing.T) {
	ctx := context.Background()
	const (
		file = "/etc/nginx/sites-enabled/a.test"
		keep = "/etc/nginx/sites-available/a.test.layte-orig"
		src  = "/etc/nginx/sites-available/a.test"
		link = "/etc/nginx/sites-enabled/a.test"
	)

	t.Run("regular file is kept aside, then put back by undo", func(t *testing.T) {
		fsys, _ := newTestFS(t)
		fsys.WriteFileAtomic(file, []byte(adminVhost), 0o640)
		r := scriptedRunner()
		undo, err := ReplaceConfig(ctx, fsys, r, replaceSpec(file, keep))
		if err != nil {
			t.Fatal(err)
		}
		if ok, _ := fsys.Exists(file); ok {
			t.Error("old file still enabled")
		}
		if b, _ := fsys.ReadFile(keep); string(b) != adminVhost {
			t.Errorf("kept copy = %q", b)
		}
		if b, _ := fsys.ReadFile(enab); !strings.Contains(string(b), "layte") {
			t.Errorf("new vhost not enabled: %q", b)
		}
		if n := len(r.CallList()); n != 2 {
			t.Errorf("want one test + one reload, got %v", r.CallList())
		}
		if err := undo(ctx); err != nil {
			t.Fatal(err)
		}
		if b, _ := fsys.ReadFile(file); string(b) != adminVhost {
			t.Errorf("not restored: %q", b)
		}
		for _, p := range []string{keep, avail, enab} {
			if ok, _ := fsys.Exists(p); ok {
				t.Errorf("%s left behind", p)
			}
		}
	})

	t.Run("symlink is removed, its target untouched", func(t *testing.T) {
		fsys, _ := newTestFS(t)
		fsys.WriteFileAtomic(src, []byte(adminVhost), 0o644)
		fsys.Symlink(src, link)
		if _, err := ReplaceConfig(ctx, fsys, scriptedRunner(), replaceSpec(link, "")); err != nil {
			t.Fatal(err)
		}
		if _, isLink, err := fsys.LinkTarget(link); err == nil || isLink {
			t.Error("old link still there")
		}
		if b, _ := fsys.ReadFile(src); string(b) != adminVhost {
			t.Error("link target changed")
		}
	})

	t.Run("failed test restores everything", func(t *testing.T) {
		fsys, _ := newTestFS(t)
		fsys.WriteFileAtomic(file, []byte(adminVhost), 0o644)
		r := scriptedRunner("/usr/sbin/nginx -t")
		if _, err := ReplaceConfig(ctx, fsys, r, replaceSpec(file, keep)); err == nil {
			t.Fatal("expected error")
		}
		if b, _ := fsys.ReadFile(file); string(b) != adminVhost {
			t.Errorf("old file not restored: %q", b)
		}
		for _, p := range []string{keep, avail, enab} {
			if ok, _ := fsys.Exists(p); ok {
				t.Errorf("%s left behind", p)
			}
		}
	})

	t.Run("refuses a changed or managed file", func(t *testing.T) {
		fsys, _ := newTestFS(t)
		fsys.WriteFileAtomic(file, []byte("server { changed; }\n"), 0o644)
		if _, err := ReplaceConfig(ctx, fsys, scriptedRunner(), replaceSpec(file, keep)); err == nil || !strings.Contains(err.Error(), "changed") {
			t.Errorf("err = %v", err)
		}
		fsys.WriteFileAtomic(file, []byte(Marker+"\n"), 0o644)
		s := replaceSpec(file, keep)
		s.Expected = []byte(Marker + "\n")
		if _, err := ReplaceConfig(ctx, fsys, scriptedRunner(), s); err == nil {
			t.Error("replaced a Layte file")
		}
	})
}
