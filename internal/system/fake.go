package system

import (
	"context"
	"fmt"
	"sync"
)

// FakeRunner is a Runner for tests and dev mode. Handle decides the output for
// each call. Missing Handle means every binary is "not installed".
type FakeRunner struct {
	Handle func(c Cmd) (Result, error)

	mu    sync.Mutex
	Calls []string // "bin arg1 arg2" (never stdin), for assertions
	Cmds  []Cmd    // full commands, including Stdin, for secret-handling tests
}

func (f *FakeRunner) Run(ctx context.Context, bin string, args ...string) (Result, error) {
	return f.RunCmd(ctx, Cmd{Bin: bin, Args: args})
}

func (f *FakeRunner) RunCmd(_ context.Context, c Cmd) (Result, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, c.String())
	f.Cmds = append(f.Cmds, c)
	f.mu.Unlock()
	if f.Handle == nil {
		return Result{}, fmt.Errorf("run %s: %w", c.Bin, ErrNotFound)
	}
	return f.Handle(c)
}

// CallList returns a copy of the recorded calls (safe while commands run).
func (f *FakeRunner) CallList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.Calls...)
}

// MapHandler builds a Handle func from a map of "bin args..." → stdout.
// Commands not in the map return ErrNotFound.
func MapHandler(outputs map[string]string) func(Cmd) (Result, error) {
	return func(c Cmd) (Result, error) {
		out, ok := outputs[c.String()]
		if !ok {
			return Result{}, fmt.Errorf("run %s: %w", c.Bin, ErrNotFound)
		}
		return Result{Stdout: []byte(out)}, nil
	}
}
