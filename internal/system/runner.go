// Package system holds thin wrappers around the host: running commands and
// reading files. Actions depend on the small interfaces here, so tests (and
// `agent --dev`) can swap in fakes and never touch the real machine.
package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout applies when the caller's context has no deadline.
const DefaultTimeout = 10 * time.Second

// ErrNotFound means the binary does not exist (e.g. the package isn't installed).
var ErrNotFound = errors.New("binary not found")

// Result is the captured output of a command.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner runs a program. An interface is just a set of method signatures; any
// type with these methods satisfies it automatically (no "implements" keyword).
// It's declared here because both the real and the fake runner live here, and
// actions accept it so they can be unit-tested.
type Runner interface {
	Run(ctx context.Context, bin string, args ...string) (Result, error)
}

// ExitError is returned when the command ran but exited non-zero.
// The Result is still returned alongside it.
type ExitError struct {
	Bin      string
	ExitCode int
	Stderr   string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("%s exited %d: %s", e.Bin, e.ExitCode, e.Stderr)
}

// ExecRunner runs real commands. Never through a shell: bin must be an absolute
// path and args are passed as a list, so nothing is ever re-parsed.
type ExecRunner struct{}

// cleanEnv gives every command a fixed, minimal environment so output is
// predictable (C locale) and nothing leaks from the agent's own environment.
var cleanEnv = []string{
	"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	"LANG=C.UTF-8",
	"LC_ALL=C.UTF-8",
}

func (ExecRunner) Run(ctx context.Context, bin string, args ...string) (Result, error) {
	if !filepath.IsAbs(bin) {
		return Result{}, fmt.Errorf("run %s: binary path must be absolute", bin)
	}
	if _, err := os.Stat(bin); errors.Is(err, fs.ErrNotExist) {
		return Result{}, fmt.Errorf("run %s: %w", bin, ErrNotFound)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = cleanEnv
	cmd.Dir = "/"
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var ee *exec.ExitError
	switch {
	case err == nil:
		return res, nil
	case ctx.Err() != nil:
		return res, fmt.Errorf("run %s: %w", bin, ctx.Err())
	case errors.As(err, &ee):
		res.ExitCode = ee.ExitCode()
		return res, &ExitError{Bin: bin, ExitCode: res.ExitCode, Stderr: strings.TrimSpace(firstLine(stderr.String()))}
	default:
		return res, fmt.Errorf("run %s: %w", bin, err)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
