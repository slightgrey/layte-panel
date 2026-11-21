package system

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
)

// DiskUsage is filesystem capacity in bytes.
type DiskUsage struct {
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"` // available to unprivileged users
}

// FS is the view of the host that actions use. Paths are absolute Linux paths
// like "/etc/nginx/sites-available/x.conf", always relative to the FS root.
type FS interface {
	ReadFile(name string) ([]byte, error)
	Glob(pattern string) ([]string, error)
	Statfs(path string) (DiskUsage, error)
	Lstat(name string) (fs.FileInfo, error)
	Exists(name string) (bool, error)
	// LinkTarget reports whether name is a symlink and where it points.
	LinkTarget(name string) (target string, isLink bool, err error)
	WriteFileAtomic(name string, data []byte, perm fs.FileMode) error
	MkdirAll(name string, perm fs.FileMode) error
	Remove(name string) error
	RemoveAll(name string) error
	Symlink(target, link string) error
	Chown(name string, uid, gid int) error
	Chmod(name string, perm fs.FileMode) error
}

// RootFS implements FS on top of os.Root (Go 1.24+). An os.Root can't reach
// anything outside its directory, even through ".." or symlinks, so the same
// code is safe whether the root is "/" (production), ./.dev/fakeroot (dev)
// or a test's temp dir.
type RootFS struct {
	root   *os.Root
	isHost bool // root is the real "/": use real statfs

	// Dev options.
	NoChown         bool // skip chown (dev: users don't really exist)
	EmulateSymlinks bool // Windows without symlink privilege: store links as pointer files
}

// OpenRootFS opens dir as the filesystem root.
func OpenRootFS(dir string) (*RootFS, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", dir, err)
	}
	return &RootFS{root: r, isHost: dir == "/"}, nil
}

// Close releases the root directory handle.
func (r *RootFS) Close() error { return r.root.Close() }

// rel turns "/etc/x" into "etc/x" (os.Root wants paths relative to the root).
func rel(name string) string {
	r := strings.TrimPrefix(path.Clean("/"+name), "/")
	if r == "" {
		return "."
	}
	return r
}

const devLinkMagic = "LAYTE-DEV-SYMLINK\n"

const maxLinkHops = 8

// resolve follows symlinks in the final path component. os.Root treats
// absolute link targets as escaping the root, but on a real server most
// sites-enabled links are absolute (/etc/nginx/sites-available/...), so they
// are resolved here: an absolute target is re-read relative to our root.
func (r *RootFS) resolve(name string) (string, error) {
	p := "/" + rel(name)
	for i := 0; i < maxLinkHops; i++ {
		target, isLink, err := r.LinkTarget(p)
		if err != nil || !isLink {
			return p, err
		}
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(p), target)
		}
		p = "/" + rel(target)
	}
	return "", fmt.Errorf("%s: too many levels of symbolic links", name)
}

func (r *RootFS) ReadFile(name string) ([]byte, error) {
	p, err := r.resolve(name)
	if err != nil {
		return nil, err
	}
	return r.root.ReadFile(rel(p))
}

func (r *RootFS) Glob(pattern string) ([]string, error) {
	m, err := fs.Glob(r.root.FS(), rel(pattern))
	if err != nil {
		return nil, err
	}
	for i := range m {
		m[i] = "/" + m[i]
	}
	return m, nil
}

func (r *RootFS) Lstat(name string) (fs.FileInfo, error) { return r.root.Lstat(rel(name)) }

func (r *RootFS) Exists(name string) (bool, error) {
	_, err := r.root.Lstat(rel(name))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (r *RootFS) LinkTarget(name string) (string, bool, error) {
	fi, err := r.root.Lstat(rel(name))
	if err != nil {
		return "", false, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		t, err := r.root.Readlink(rel(name))
		return t, err == nil, err
	}
	if r.EmulateSymlinks && fi.Mode().IsRegular() && fi.Size() < 4096 {
		b, err := r.root.ReadFile(rel(name))
		if err == nil && bytes.HasPrefix(b, []byte(devLinkMagic)) {
			return strings.TrimSpace(string(b[len(devLinkMagic):])), true, nil
		}
	}
	return "", false, nil
}

// WriteFileAtomic writes to a temp file in the same directory, fsyncs it and
// renames it over name, so readers see either the old or the new file, never
// a half-written one.
func (r *RootFS) WriteFileAtomic(name string, data []byte, perm fs.FileMode) error {
	dir := path.Dir(rel(name))
	b := make([]byte, 6)
	rand.Read(b)
	tmp := path.Join(dir, ".layte-tmp-"+hex.EncodeToString(b))

	f, err := r.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", name, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		r.root.Remove(tmp)
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		r.root.Remove(tmp)
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		r.root.Remove(tmp)
		return err
	}
	if err := r.root.Chmod(tmp, perm); err != nil { // umask may have narrowed it
		r.root.Remove(tmp)
		return err
	}
	if err := r.root.Rename(tmp, rel(name)); err != nil {
		r.root.Remove(tmp)
		return fmt.Errorf("rename into %s: %w", name, err)
	}
	return nil
}

func (r *RootFS) MkdirAll(name string, perm fs.FileMode) error {
	return r.root.MkdirAll(rel(name), perm)
}

func (r *RootFS) Remove(name string) error { return r.root.Remove(rel(name)) }

func (r *RootFS) RemoveAll(name string) error { return r.root.RemoveAll(rel(name)) }

func (r *RootFS) Symlink(target, link string) error {
	if r.EmulateSymlinks {
		f, err := r.root.OpenFile(rel(link), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		_, err = f.WriteString(devLinkMagic + target + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}
	return r.root.Symlink(target, rel(link))
}

func (r *RootFS) Chown(name string, uid, gid int) error {
	if r.NoChown {
		return nil
	}
	return r.root.Lchown(rel(name), uid, gid)
}

func (r *RootFS) Chmod(name string, perm fs.FileMode) error { return r.root.Chmod(rel(name), perm) }

func (r *RootFS) Statfs(p string) (DiskUsage, error) {
	if r.isHost {
		return hostStatfs(p)
	}
	// Dev/test roots: canned numbers from statfs.json at the root.
	b, err := r.root.ReadFile("statfs.json")
	if err != nil {
		return DiskUsage{}, err
	}
	var all map[string]DiskUsage
	if err := json.Unmarshal(b, &all); err != nil {
		return DiskUsage{}, fmt.Errorf("statfs.json: %w", err)
	}
	u, ok := all[p]
	if !ok {
		return DiskUsage{}, fmt.Errorf("statfs %s: %w", p, fs.ErrNotExist)
	}
	return u, nil
}
