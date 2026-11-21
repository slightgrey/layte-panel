package system

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestExecRunnerRejectsRelativePath(t *testing.T) {
	for _, bin := range []string{"nginx", "./nginx", "sh"} {
		if _, err := (ExecRunner{}).Run(context.Background(), bin, "-v"); err == nil {
			t.Errorf("Run(%q) should fail: binary path must be absolute", bin)
		}
	}
}

func TestExecRunnerNotFound(t *testing.T) {
	_, err := (ExecRunner{}).Run(context.Background(), filepath.Join(t.TempDir(), "nginx"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestMapHandler(t *testing.T) {
	f := &FakeRunner{Handle: MapHandler(map[string]string{"/usr/bin/x --version": "x 1.0"})}
	res, err := f.Run(context.Background(), "/usr/bin/x", "--version")
	if err != nil || string(res.Stdout) != "x 1.0" {
		t.Fatalf("got %q, %v", res.Stdout, err)
	}
	if _, err := f.Run(context.Background(), "/usr/bin/y"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown command err = %v", err)
	}
	if len(f.Calls) != 2 {
		t.Errorf("calls = %v", f.Calls)
	}
}
