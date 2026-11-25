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

// ReplaceSpec describes swapping a config file Layte didn't create (the
// admin's nginx vhost of an adopted site) for one Layte renders.
type ReplaceSpec struct {
	New WriteSpec // the Layte file (+ its enable link); must not exist yet
	// Old is the admin's file as the service loads it: a regular file, or a
	// symlink (e.g. sites-enabled/x → sites-available/x).
	Old string
	// Keep is where a regular Old file is moved, so the service no longer
	// loads it but the admin keeps it (unused when Old is a symlink: only
	// the link is removed; its target stays where it is).
	Keep string
	// Expected is the content the admin saw in the preview.
	Expected []byte
}

// ReplaceConfig disables the old file and installs the new one, with one
// config test and reload for both, so the two are never loaded together. On
// any failure everything is put back. The returned undo does the same after
// success (used when a later check fails).
func ReplaceConfig(ctx context.Context, fsys FS, r Runner, s ReplaceSpec) (undo func(context.Context) error, err error) {
	n := s.New
	if !IsManaged(n.Content) {
		return nil, fmt.Errorf("replace %s: content must start with the Layte marker", s.Old)
	}
	if n.Perm == 0 {
		n.Perm = 0o644
	}
	configMu.Lock()
	defer configMu.Unlock()

	old, err := fsys.ReadFile(s.Old) // through the link, if it is one
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.Old, err)
	}
	if IsManaged(old) {
		return nil, fmt.Errorf("%s is already managed by Layte Panel", s.Old)
	}
	if string(old) != string(s.Expected) {
		return nil, fmt.Errorf("%s changed since the preview; review the changes again", s.Old)
	}
	target, isLink, err := fsys.LinkTarget(s.Old)
	if err != nil {
		return nil, fmt.Errorf("check %s: %w", s.Old, err)
	}
	var perm fs.FileMode = 0o644
	if !isLink {
		fi, err := fsys.Lstat(s.Old)
		if err != nil || !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", s.Old)
		}
		perm = fi.Mode().Perm()
		if s.Keep == "" {
			return nil, fmt.Errorf("replace %s: no place to keep the original", s.Old)
		}
		if ok, _ := fsys.Exists(s.Keep); ok {
			return nil, fmt.Errorf("%s already exists; move it away first", s.Keep)
		}
	}
	for _, p := range []string{n.Path, n.Link} {
		if ok, _ := fsys.Exists(p); p != "" && ok {
			return nil, fmt.Errorf("%s already exists; move it away first", p)
		}
	}

	// Put the old file back and the new one away (best effort, idempotent).
	restore := func() {
		if n.Link != "" {
			fsys.Remove(n.Link)
		}
		fsys.Remove(n.Path)
		if isLink {
			if ok, _ := fsys.Exists(s.Old); !ok {
				fsys.Symlink(target, s.Old)
			}
		} else if ok, _ := fsys.Exists(s.Keep); ok {
			if fsys.WriteFileAtomic(s.Old, old, perm) == nil {
				fsys.Remove(s.Keep)
			}
		}
	}

	if !isLink {
		if err := fsys.WriteFileAtomic(s.Keep, old, perm); err != nil {
			return nil, fmt.Errorf("keep original %s: %w", s.Old, err)
		}
	}
	if err := fsys.Remove(s.Old); err != nil {
		restore()
		return nil, fmt.Errorf("disable %s: %w", s.Old, err)
	}
	if err := fsys.WriteFileAtomic(n.Path, n.Content, n.Perm); err != nil {
		restore()
		return nil, fmt.Errorf("write %s: %w", n.Path, err)
	}
	if n.Link != "" {
		if err := fsys.Symlink(n.Path, n.Link); err != nil {
			restore()
			return nil, fmt.Errorf("enable %s: %w", n.Link, err)
		}
	}
	if n.Test.Bin != "" {
		if res, err := r.RunCmd(ctx, n.Test); err != nil {
			restore()
			return nil, &TestError{Cmd: n.Test.String(), Output: testOutput(res, err)}
		}
	}
	if n.Reload.Bin != "" {
		if res, err := r.RunCmd(ctx, n.Reload); err != nil {
			restore()
			r.RunCmd(ctx, n.Reload)
			return nil, fmt.Errorf("reload after replacing %s: %s", s.Old, testOutput(res, err))
		}
	}
	return func(ctx context.Context) error {
		configMu.Lock()
		defer configMu.Unlock()
		restore()
		if n.Test.Bin != "" {
			if res, err := r.RunCmd(ctx, n.Test); err != nil {
				return &TestError{Cmd: n.Test.String(), Output: testOutput(res, err)}
			}
		}
		if n.Reload.Bin != "" {
			if res, err := r.RunCmd(ctx, n.Reload); err != nil {
				return fmt.Errorf("reload after restoring %s: %s", s.Old, testOutput(res, err))
			}
		}
		return nil
	}, nil
}
