package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestFS(t *testing.T) (*RootFS, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := OpenRootFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	r.EmulateSymlinks = runtime.GOOS == "windows"
	t.Cleanup(func() { r.Close() })
	for _, d := range []string{"/etc/nginx/sites-available", "/etc/nginx/sites-enabled"} {
		if err := r.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return r, dir
}

// scriptedRunner fails the commands listed in fail (by "bin args" string).
func scriptedRunner(fail ...string) *FakeRunner {
	return &FakeRunner{Handle: func(c Cmd) (Result, error) {
		for _, f := range fail {
			if c.String() == f {
				return Result{Stderr: []byte("nginx: [emerg] unexpected \"}\"")}, &ExitError{Bin: c.Bin, ExitCode: 1}
			}
		}
		return Result{}, nil
	}}
}

const (
	avail = "/etc/nginx/sites-available/layte-a.test.conf"
	enab  = "/etc/nginx/sites-enabled/layte-a.test.conf"
)

var (
	testCmd   = Cmd{Bin: "/usr/sbin/nginx", Args: []string{"-t"}}
	reloadCmd = Cmd{Bin: "/usr/bin/systemctl", Args: []string{"reload", "nginx"}}
)

func spec(content string) WriteSpec {
	return WriteSpec{Path: avail, Content: []byte(content), Link: enab, Test: testCmd, Reload: reloadCmd}
}

func TestWriteConfigNewAndUpdate(t *testing.T) {
	fsys, _ := newTestFS(t)
	r := scriptedRunner()
	ctx := context.Background()

	if err := WriteConfig(ctx, fsys, r, spec(Marker+"\nv1\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := fsys.ReadFile(enab); !strings.Contains(string(b), "v1") {
		t.Errorf("enabled link doesn't resolve to the new file: %q", b)
	}
	if err := WriteConfig(ctx, fsys, r, spec(Marker+"\nv2\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := fsys.ReadFile(avail); !strings.Contains(string(b), "v2") {
		t.Errorf("not updated: %q", b)
	}
	if ok, _ := fsys.Exists(avail + ".layte-bak"); ok {
		t.Error("backup left behind")
	}
	want := []string{"/usr/sbin/nginx -t", "/usr/bin/systemctl reload nginx", "/usr/sbin/nginx -t", "/usr/bin/systemctl reload nginx"}
	if got := r.CallList(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %v", got)
	}
	// Writing identical content again is a no-op (no reload).
	WriteConfig(ctx, fsys, r, spec(Marker+"\nv2\n"))
	if n := len(r.CallList()); n != 4 {
		t.Errorf("no-op write ran commands: %d", n)
	}
}

func TestWriteConfigTestFailureRollsBack(t *testing.T) {
	ctx := context.Background()

	t.Run("new file is removed", func(t *testing.T) {
		fsys, _ := newTestFS(t)
		r := scriptedRunner("/usr/sbin/nginx -t")
		err := WriteConfig(ctx, fsys, r, spec(Marker+"\nbroken\n"))
		var te *TestError
		if !errors.As(err, &te) || !strings.Contains(te.Output, "emerg") {
			t.Fatalf("err = %v", err)
		}
		for _, p := range []string{avail, enab} {
			if ok, _ := fsys.Exists(p); ok {
				t.Errorf("%s left behind", p)
			}
		}
		for _, c := range r.CallList() {
			if strings.Contains(c, "reload") {
				t.Error("reloaded despite failed test")
			}
		}
	})

	t.Run("update restores old content", func(t *testing.T) {
		fsys, _ := newTestFS(t)
		WriteConfig(ctx, fsys, scriptedRunner(), spec(Marker+"\ngood\n"))
		err := WriteConfig(ctx, fsys, scriptedRunner("/usr/sbin/nginx -t"), spec(Marker+"\nbroken\n"))
		if err == nil {
			t.Fatal("expected error")
		}
		if b, _ := fsys.ReadFile(avail); !strings.Contains(string(b), "good") {
			t.Errorf("old content not restored: %q", b)
		}
		if ok, _ := fsys.Exists(enab); !ok {
			t.Error("existing link was removed")
		}
	})

	t.Run("reload failure restores and reloads again", func(t *testing.T) {
		fsys, _ := newTestFS(t)
		r := scriptedRunner("/usr/bin/systemctl reload nginx")
		if err := WriteConfig(ctx, fsys, r, spec(Marker+"\nx\n")); err == nil {
			t.Fatal("expected error")
		}
		if ok, _ := fsys.Exists(avail); ok {
			t.Error("file left behind")
		}
		reloads := 0
		for _, c := range r.CallList() {
			if strings.Contains(c, "reload") {
				reloads++
			}
		}
		if reloads != 2 {
			t.Errorf("reloads = %d, want 2", reloads)
		}
	})
}

func TestWriteConfigRefusesForeignFiles(t *testing.T) {
	ctx := context.Background()
	fsys, dir := newTestFS(t)
	foreign := "server { listen 80; } # managed by Certbot\n"
	os.WriteFile(filepath.Join(dir, "etc/nginx/sites-available/layte-a.test.conf"), []byte(foreign), 0o644)
	r := scriptedRunner()

	if err := WriteConfig(ctx, fsys, r, spec(Marker+"\nmine\n")); !errors.Is(err, ErrNotManaged) {
		t.Errorf("WriteConfig err = %v, want ErrNotManaged", err)
	}
	if err := RemoveConfig(ctx, fsys, r, RemoveSpec{Path: avail, Test: testCmd, Reload: reloadCmd}); !errors.Is(err, ErrNotManaged) {
		t.Errorf("RemoveConfig err = %v, want ErrNotManaged", err)
	}
	if b, _ := fsys.ReadFile(avail); string(b) != foreign {
		t.Errorf("foreign file changed: %q", b)
	}
	if len(r.CallList()) != 0 {
		t.Errorf("commands ran: %v", r.CallList())
	}
	// Content without the marker is rejected outright.
	if err := WriteConfig(ctx, fsys, r, WriteSpec{Path: "/etc/nginx/sites-available/other", Content: []byte("x")}); err == nil {
		t.Error("unmarked content accepted")
	}
}

func TestWriteConfigLinkPointingElsewhere(t *testing.T) {
	fsys, _ := newTestFS(t)
	fsys.WriteFileAtomic("/etc/nginx/sites-available/other.conf", []byte("x"), 0o644)
	fsys.Symlink("/etc/nginx/sites-available/other.conf", enab)
	if err := WriteConfig(context.Background(), fsys, scriptedRunner(), spec(Marker+"\n")); err == nil {
		t.Error("expected refusal when the enable link points to another file")
	}
}

func TestRemoveConfig(t *testing.T) {
	ctx := context.Background()
	fsys, _ := newTestFS(t)
	WriteConfig(ctx, fsys, scriptedRunner(), spec(Marker+"\nx\n"))
	rs := RemoveSpec{Path: avail, Link: enab, Test: testCmd, Reload: reloadCmd}

	// Failed test → restored.
	if err := RemoveConfig(ctx, fsys, scriptedRunner("/usr/sbin/nginx -t"), rs); err == nil {
		t.Fatal("expected error")
	}
	for _, p := range []string{avail, enab} {
		if ok, _ := fsys.Exists(p); !ok {
			t.Errorf("%s not restored", p)
		}
	}
	if err := RemoveConfig(ctx, fsys, scriptedRunner(), rs); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{avail, enab} {
		if ok, _ := fsys.Exists(p); ok {
			t.Errorf("%s still exists", p)
		}
	}
	// Idempotent.
	if err := RemoveConfig(ctx, fsys, scriptedRunner(), rs); err != nil {
		t.Errorf("second remove: %v", err)
	}
}

func TestRootFSJail(t *testing.T) {
	fsys, dir := newTestFS(t)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.txt"), []byte("secret"), 0o644)
	// ".." can't climb out: the path is cleaned against the root.
	if b, err := fsys.ReadFile("/../outside.txt"); err == nil {
		t.Errorf("read outside root: %q", b)
	}
	if runtime.GOOS != "windows" {
		// A relative symlink that escapes must be refused by os.Root.
		os.Symlink("../../outside.txt", filepath.Join(dir, "etc/escape"))
		if b, err := fsys.ReadFile("/etc/escape"); err == nil {
			t.Errorf("followed escaping symlink: %q", b)
		}
	}
	// Absolute link targets are re-rooted, not followed on the host.
	fsys.WriteFileAtomic("/etc/real.conf", []byte("inside"), 0o644)
	fsys.Symlink("/etc/real.conf", "/etc/link.conf")
	if b, err := fsys.ReadFile("/etc/link.conf"); err != nil || string(b) != "inside" {
		t.Errorf("absolute link: %q %v", b, err)
	}
}
