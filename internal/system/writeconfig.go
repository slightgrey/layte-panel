package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
)

// Marker starts every config file Layte Panel writes. Files without it are
// never overwritten or deleted: they belong to the admin (or Certbot).
// MarkerINI is the same for INI-style files (PHP-FPM pools reject "#").
const (
	Marker    = "# Managed by Layte Panel"
	MarkerINI = "; Managed by Layte Panel"
)

// ErrNotManaged is returned when asked to change a file Layte didn't create.
var ErrNotManaged = errors.New("file exists and is not managed by Layte Panel")

// TestError means the service's config test (nginx -t, php-fpm -t) failed.
// The change was rolled back; Output says what was wrong.
type TestError struct {
	Cmd    string
	Output string
}

func (e *TestError) Error() string { return fmt.Sprintf("%s failed: %s", e.Cmd, e.Output) }

// IsManaged reports whether content starts with a Layte marker.
func IsManaged(content []byte) bool {
	return bytes.HasPrefix(content, []byte(Marker)) || bytes.HasPrefix(content, []byte(MarkerINI))
}

// WriteSpec describes one config change.
type WriteSpec struct {
	Path    string      // e.g. /etc/nginx/sites-available/layte-example.com.conf
	Content []byte      // must start with Marker
	Perm    fs.FileMode // default 0644
	Link    string      // optional symlink to create → Path (e.g. sites-enabled/...)
	Test    Cmd         // e.g. /usr/sbin/nginx -t
	Reload  Cmd         // e.g. /usr/bin/systemctl reload nginx
}

// configMu serialises config changes: `nginx -t` checks the whole config, so
// two concurrent changes could blame (and roll back) each other.
var configMu sync.Mutex

// WriteConfig safely installs a config file:
//
//  1. refuse if an existing file isn't ours
//  2. back up the current file (<path>.layte-bak)
//  3. write a temp file and atomically rename it into place (+ enable symlink)
//  4. run the config test; on failure restore the backup and stop
//  5. reload the service; on failure restore and reload again
//
// (`nginx -t` only checks the live config, which is why the test runs after
// the rename rather than against the temp file.) One bad template can never
// leave a broken config in place for other sites.
func WriteConfig(ctx context.Context, fsys FS, r Runner, s WriteSpec) error {
	if !IsManaged(s.Content) {
		return fmt.Errorf("write %s: content must start with the Layte marker", s.Path)
	}
	if s.Perm == 0 {
		s.Perm = 0o644
	}
	configMu.Lock()
	defer configMu.Unlock()

	old, existed, err := readManaged(fsys, s.Path)
	if err != nil {
		return err
	}
	linkCreated := false
	if s.Link != "" {
		target, isLink, err := fsys.LinkTarget(s.Link)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// will create below
		case err != nil:
			return fmt.Errorf("check %s: %w", s.Link, err)
		case !isLink || target != s.Path:
			return fmt.Errorf("%s exists and doesn't point to %s; refusing to change it", s.Link, s.Path)
		}
	}
	if existed && bytes.Equal(old, s.Content) {
		if s.Link == "" {
			return nil
		}
		if ok, _ := fsys.Exists(s.Link); ok {
			return nil // nothing to do
		}
	}

	backup := s.Path + ".layte-bak"
	if existed {
		if err := fsys.WriteFileAtomic(backup, old, s.Perm); err != nil {
			return fmt.Errorf("backup %s: %w", s.Path, err)
		}
	}
	rollback := func() {
		if existed {
			fsys.WriteFileAtomic(s.Path, old, s.Perm)
		} else {
			fsys.Remove(s.Path)
		}
		if linkCreated {
			fsys.Remove(s.Link)
		}
		fsys.Remove(backup)
	}

	if err := fsys.WriteFileAtomic(s.Path, s.Content, s.Perm); err != nil {
		fsys.Remove(backup)
		return fmt.Errorf("write %s: %w", s.Path, err)
	}
	if s.Link != "" {
		if ok, _ := fsys.Exists(s.Link); !ok {
			if err := fsys.Symlink(s.Path, s.Link); err != nil {
				rollback()
				return fmt.Errorf("enable %s: %w", s.Link, err)
			}
			linkCreated = true
		}
	}

	if s.Test.Bin == "" {
		// nothing to test (e.g. cron.d files)
	} else if res, err := r.RunCmd(ctx, s.Test); err != nil {
		rollback()
		return &TestError{Cmd: s.Test.String(), Output: testOutput(res, err)}
	}
	if s.Reload.Bin == "" {
		// the service picks up changes by itself
	} else if res, err := r.RunCmd(ctx, s.Reload); err != nil {
		rollback()
		r.RunCmd(ctx, s.Reload) // bring the service back on the old config
		return fmt.Errorf("reload after writing %s: %s", s.Path, testOutput(res, err))
	}
	fsys.Remove(backup)
	return nil
}

// RemoveSpec describes removing a config file Layte created.
type RemoveSpec struct {
	Path   string
	Link   string // optional enable-symlink to remove too
	Test   Cmd
	Reload Cmd
}

// RemoveConfig deletes a managed config (and its symlink), tests, and reloads.
// It is idempotent: a missing file is not an error. On a failed test the file
// is restored.
func RemoveConfig(ctx context.Context, fsys FS, r Runner, s RemoveSpec) error {
	configMu.Lock()
	defer configMu.Unlock()

	old, existed, err := readManaged(fsys, s.Path)
	if err != nil {
		return err
	}
	linkRemoved := false
	if s.Link != "" {
		if target, isLink, err := fsys.LinkTarget(s.Link); err == nil {
			if !isLink || target != s.Path {
				return fmt.Errorf("%s doesn't point to %s; refusing to remove it", s.Link, s.Path)
			}
			if err := fsys.Remove(s.Link); err != nil {
				return fmt.Errorf("remove %s: %w", s.Link, err)
			}
			linkRemoved = true
		}
	}
	if !existed && !linkRemoved {
		return nil
	}
	if existed {
		if err := fsys.Remove(s.Path); err != nil {
			return fmt.Errorf("remove %s: %w", s.Path, err)
		}
	}
	restore := func() {
		if existed {
			fsys.WriteFileAtomic(s.Path, old, 0o644)
		}
		if linkRemoved {
			fsys.Symlink(s.Path, s.Link)
		}
	}
	if s.Test.Bin == "" {
		return nil
	}
	if res, err := r.RunCmd(ctx, s.Test); err != nil {
		restore()
		return &TestError{Cmd: s.Test.String(), Output: testOutput(res, err)}
	}
	if res, err := r.RunCmd(ctx, s.Reload); err != nil {
		return fmt.Errorf("reload after removing %s: %s", s.Path, testOutput(res, err))
	}
	return nil
}

// readManaged reads path; it errors if the file exists without our marker.
func readManaged(fsys FS, p string) (content []byte, existed bool, err error) {
	b, err := fsys.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", p, err)
	}
	if !IsManaged(b) {
		return nil, true, fmt.Errorf("%s: %w", p, ErrNotManaged)
	}
	return b, true, nil
}

func testOutput(res Result, err error) string {
	out := res.Output()
	if out == "" {
		return err.Error()
	}
	if len(out) > 2000 {
		out = out[:2000] + "…"
	}
	return strings.TrimSpace(out)
}
