package system

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
)

// AdoptSuffix is appended to a config file's path for the untouched original
// Layte keeps when it takes the file over. It is never deleted automatically,
// so the admin can always go back by hand.
const AdoptSuffix = ".layte-orig"

// AdoptConfig takes over a config file Layte didn't create (used only when
// the admin explicitly adopts a site, after seeing the diff):
//
//  1. refuse if the file is already Layte's, or the original copy exists
//  2. keep the original as <path>.layte-orig
//  3. write the new content (which must start with Marker)
//  4. run the config test, then reload; on any failure put the original back
//     (and reload again) and remove the copy
//
// expected is the content the diff was shown for: if the file changed since,
// nothing is written.
func AdoptConfig(ctx context.Context, fsys FS, r Runner, s WriteSpec, expected []byte) error {
	if !IsManaged(s.Content) {
		return fmt.Errorf("adopt %s: content must start with the Layte marker", s.Path)
	}
	configMu.Lock()
	defer configMu.Unlock()

	fi, err := fsys.Lstat(s.Path)
	if err != nil {
		return fmt.Errorf("adopt %s: %w", s.Path, err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("adopt %s: not a regular file", s.Path)
	}
	old, err := fsys.ReadFile(s.Path)
	if err != nil {
		return err
	}
	if IsManaged(old) {
		return fmt.Errorf("adopt %s: already managed by Layte Panel", s.Path)
	}
	if string(old) != string(expected) {
		return fmt.Errorf("adopt %s: the file changed since the preview; review the changes again", s.Path)
	}
	orig := s.Path + AdoptSuffix
	if ok, _ := fsys.Exists(orig); ok {
		return fmt.Errorf("adopt %s: %s already exists; move it away first", s.Path, orig)
	}
	perm := fi.Mode().Perm()
	if err := fsys.WriteFileAtomic(orig, old, perm); err != nil {
		return fmt.Errorf("keep original %s: %w", s.Path, err)
	}
	restore := func() {
		fsys.WriteFileAtomic(s.Path, old, perm)
		fsys.Remove(orig)
	}
	if err := fsys.WriteFileAtomic(s.Path, s.Content, perm); err != nil {
		fsys.Remove(orig)
		return fmt.Errorf("write %s: %w", s.Path, err)
	}
	if s.Test.Bin != "" {
		if res, err := r.RunCmd(ctx, s.Test); err != nil {
			restore()
			return &TestError{Cmd: s.Test.String(), Output: testOutput(res, err)}
		}
	}
	if s.Reload.Bin != "" {
		if res, err := r.RunCmd(ctx, s.Reload); err != nil {
			restore()
			r.RunCmd(ctx, s.Reload)
			return fmt.Errorf("reload after adopting %s: %s", s.Path, testOutput(res, err))
		}
	}
	return nil
}

// RestoreAdopted puts an adopted file's original back (used to undo an
// adoption that failed after AdoptConfig succeeded), tests and reloads.
func RestoreAdopted(ctx context.Context, fsys FS, r Runner, path string, test, reload Cmd) error {
	configMu.Lock()
	defer configMu.Unlock()
	orig := path + AdoptSuffix
	b, err := fsys.ReadFile(orig)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("restore %s: no %s", path, orig)
	} else if err != nil {
		return err
	}
	perm := fs.FileMode(0o644)
	if fi, err := fsys.Lstat(orig); err == nil {
		perm = fi.Mode().Perm()
	}
	if err := fsys.WriteFileAtomic(path, b, perm); err != nil {
		return err
	}
	fsys.Remove(orig)
	if test.Bin != "" {
		if res, err := r.RunCmd(ctx, test); err != nil {
			return &TestError{Cmd: test.String(), Output: testOutput(res, err)}
		}
	}
	if reload.Bin != "" {
		if res, err := r.RunCmd(ctx, reload); err != nil {
			return fmt.Errorf("reload after restoring %s: %s", path, testOutput(res, err))
		}
	}
	return nil
}
