// Package fileops implements the file manager's operations inside an
// os.Root jail. It runs in a short-lived helper process (`layte fsop`) that
// the agent starts AS THE SITE'S USER, so the kernel's normal permission
// checks apply and nothing ever happens as root. os.Root additionally makes
// sure no path (including via "..", or symlinks) can leave the site folder.
package fileops

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits.
const (
	MaxChunk     = 512 << 10 // per read/write call
	MaxEditSize  = 2 << 20   // files the editor will open
	uploadPrefix = ".layte-upload-"
)

// Request is one operation. Root is the jail; Path/To are relative to it.
type Request struct {
	Op       string `json:"op"` // list | read | write | mkdir | delete | rename | stat
	Root     string `json:"root"`
	Path     string `json:"path"`
	To       string `json:"to,omitempty"`     // rename target
	Offset   int64  `json:"offset,omitempty"` // read/write position
	Length   int    `json:"length,omitempty"` // read size
	Chunk    []byte `json:"chunk,omitempty"`  // write data (JSON: base64)
	Final    bool   `json:"final,omitempty"`  // write: last chunk, move into place
	ReadOnly bool   `json:"read_only,omitempty"`
}

// Entry is one directory item.
type Entry struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"` // dir | file | symlink | other
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"mtime"`
}

// Response is the result of a Request.
type Response struct {
	Entries []Entry `json:"entries,omitempty"`
	Data    []byte  `json:"data,omitempty"`
	Size    int64   `json:"size"`
	Text    bool    `json:"text,omitempty"` // read: data is valid UTF-8 text
	Entry   *Entry  `json:"entry,omitempty"`
	Error   string  `json:"error,omitempty"`
}

var writeOps = map[string]bool{"write": true, "mkdir": true, "delete": true, "rename": true}

// CleanPath validates a path relative to the jail: no absolute paths, no
// "..", no NUL. "" and "." mean the jail itself.
func CleanPath(p string) (string, error) {
	if strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", errors.New("invalid path")
	}
	// Reject ".." outright rather than letting Clean quietly re-root it.
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "", errors.New("path may not contain ..")
		}
	}
	c := path.Clean("/" + strings.TrimPrefix(p, "/"))[1:]
	if c == "" {
		return ".", nil
	}
	return c, nil
}

// Do executes a request. All errors are returned in Response.Error with a
// message that is safe to show to the user.
func Do(req Request) Response {
	resp, err := do(req)
	if err != nil {
		return Response{Error: friendly(err)}
	}
	return resp
}

func do(req Request) (Response, error) {
	if req.ReadOnly && writeOps[req.Op] {
		return Response{}, errors.New("this site is read-only in Layte Panel")
	}
	p, err := CleanPath(req.Path)
	if err != nil {
		return Response{}, err
	}
	root, err := os.OpenRoot(req.Root)
	if err != nil {
		return Response{}, err
	}
	defer root.Close()

	switch req.Op {
	case "list":
		return list(root, p)
	case "stat":
		fi, err := root.Lstat(p)
		if err != nil {
			return Response{}, err
		}
		e := entryOf(fi)
		return Response{Entry: &e, Size: fi.Size()}, nil
	case "read":
		return read(root, p, req.Offset, req.Length)
	case "write":
		return write(root, p, req)
	case "mkdir":
		return Response{}, root.MkdirAll(p, 0o755)
	case "delete":
		if p == "." {
			return Response{}, errors.New("refusing to delete the site folder itself")
		}
		if _, err := root.Lstat(p); err != nil {
			return Response{}, err
		}
		return Response{}, root.RemoveAll(p)
	case "rename":
		to, err := CleanPath(req.To)
		if err != nil {
			return Response{}, err
		}
		if p == "." || to == "." {
			return Response{}, errors.New("invalid rename")
		}
		if _, err := root.Lstat(to); err == nil {
			return Response{}, fmt.Errorf("%s already exists", to)
		}
		return Response{}, root.Rename(p, to)
	}
	return Response{}, fmt.Errorf("unknown operation %q", req.Op)
}

func list(root *os.Root, p string) (Response, error) {
	f, err := root.Open(p)
	if err != nil {
		return Response{}, err
	}
	defer f.Close()
	items, err := f.ReadDir(-1)
	if err != nil {
		return Response{}, err
	}
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		if strings.HasPrefix(it.Name(), uploadPrefix) {
			continue // unfinished upload
		}
		fi, err := it.Info()
		if err != nil {
			continue
		}
		out = append(out, entryOf(fi))
	}
	// Folders first, then by name.
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Type == "dir") != (out[j].Type == "dir") {
			return out[i].Type == "dir"
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return Response{Entries: out}, nil
}

func read(root *os.Root, p string, offset int64, length int) (Response, error) {
	f, err := root.Open(p)
	if err != nil {
		return Response{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Response{}, err
	}
	if !fi.Mode().IsRegular() {
		return Response{}, errors.New("not a regular file")
	}
	if length <= 0 || length > MaxChunk {
		length = MaxChunk
	}
	buf := make([]byte, length)
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return Response{}, err
	}
	buf = buf[:n]
	return Response{Data: buf, Size: fi.Size(), Text: utf8.Valid(buf) && !containsNUL(buf)}, nil
}

// write appends a chunk to a temp file next to the target; the final chunk
// renames it into place, so a half-finished upload never replaces a file.
func write(root *os.Root, p string, req Request) (Response, error) {
	if p == "." {
		return Response{}, errors.New("invalid file name")
	}
	if len(req.Chunk) > MaxChunk {
		return Response{}, errors.New("chunk too large")
	}
	tmp := path.Join(path.Dir(p), uploadPrefix+path.Base(p))
	flags := os.O_WRONLY | os.O_CREATE
	if req.Offset == 0 {
		flags |= os.O_TRUNC
	}
	f, err := root.OpenFile(tmp, flags, 0o644)
	if err != nil {
		return Response{}, err
	}
	if _, err := f.WriteAt(req.Chunk, req.Offset); err != nil {
		f.Close()
		return Response{}, err
	}
	if err := f.Close(); err != nil {
		return Response{}, err
	}
	if !req.Final {
		return Response{Size: req.Offset + int64(len(req.Chunk))}, nil
	}
	// Keep the old file's permissions when replacing it (e.g. wp-config.php 0640).
	if fi, err := root.Stat(p); err == nil {
		root.Chmod(tmp, fi.Mode().Perm())
	}
	if err := root.Rename(tmp, p); err != nil {
		root.Remove(tmp)
		return Response{}, err
	}
	fi, err := root.Stat(p)
	if err != nil {
		return Response{}, err
	}
	e := entryOf(fi)
	return Response{Entry: &e, Size: fi.Size()}, nil
}

func entryOf(fi fs.FileInfo) Entry {
	t := "other"
	switch m := fi.Mode(); {
	case m.IsDir():
		t = "dir"
	case m.IsRegular():
		t = "file"
	case m&fs.ModeSymlink != 0:
		t = "symlink"
	}
	return Entry{Name: fi.Name(), Type: t, Size: fi.Size(), Mode: fi.Mode().Perm().String(), ModTime: fi.ModTime().UTC()}
}

func containsNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

// friendly turns OS errors into short messages without host details.
func friendly(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "no such file or folder"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case errors.Is(err, fs.ErrExist):
		return "already exists"
	}
	msg := err.Error()
	if strings.Contains(msg, "path escapes from parent") {
		return "that path leaves the site folder"
	}
	return msg
}

// Serve is the `layte fsop` helper: one JSON Request on stdin, one JSON
// Response on stdout.
func Serve(in io.Reader, out io.Writer) error {
	var req Request
	dec := json.NewDecoder(io.LimitReader(in, 2*MaxChunk))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return json.NewEncoder(out).Encode(Response{Error: "bad request"})
	}
	return json.NewEncoder(out).Encode(Do(req))
}
