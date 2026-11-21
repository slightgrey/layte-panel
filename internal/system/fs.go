package system

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DiskUsage is filesystem capacity in bytes.
type DiskUsage struct {
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"` // available to unprivileged users
}

// FS is the read-only view of the host that actions need. Paths are absolute
// Linux paths like "/proc/meminfo".
type FS interface {
	ReadFile(name string) ([]byte, error)
	Glob(pattern string) ([]string, error)
	Statfs(path string) (DiskUsage, error)
}

// HostFS reads the real machine. Statfs is implemented per-OS (statfs_*.go).
type HostFS struct{}

func (HostFS) ReadFile(name string) ([]byte, error)  { return os.ReadFile(name) }
func (HostFS) Glob(pattern string) ([]string, error) { return filepath.Glob(pattern) }

// FixtureFS serves files from an fs.FS (e.g. an embedded testdata tree), so
// "/proc/meminfo" reads "<root>/proc/meminfo". Statfs answers come from
// "<root>/statfs.json": {"/": {"total": N, "free": N}, ...}.
type FixtureFS struct {
	Root fs.FS
}

func (f FixtureFS) rel(name string) string { return strings.TrimPrefix(path.Clean(name), "/") }

func (f FixtureFS) ReadFile(name string) ([]byte, error) { return fs.ReadFile(f.Root, f.rel(name)) }

func (f FixtureFS) Glob(pattern string) ([]string, error) {
	m, err := fs.Glob(f.Root, f.rel(pattern))
	if err != nil {
		return nil, err
	}
	for i := range m {
		m[i] = "/" + m[i]
	}
	return m, nil
}

func (f FixtureFS) Statfs(p string) (DiskUsage, error) {
	b, err := fs.ReadFile(f.Root, "statfs.json")
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
