package system

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// FakeRunner is a Runner for tests and dev mode. Handle decides the output for
// each call. Missing Handle means every binary is "not installed".
type FakeRunner struct {
	Handle func(bin string, args []string) (Result, error)

	mu    sync.Mutex
	Calls []string // "bin arg1 arg2", recorded for assertions
}

func (f *FakeRunner) Run(ctx context.Context, bin string, args ...string) (Result, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, strings.TrimSpace(bin+" "+strings.Join(args, " ")))
	f.mu.Unlock()
	if f.Handle == nil {
		return Result{}, fmt.Errorf("run %s: %w", bin, ErrNotFound)
	}
	return f.Handle(bin, args)
}

// MapHandler builds a Handle func from a map of "bin args..." → stdout.
// Commands not in the map return ErrNotFound.
func MapHandler(outputs map[string]string) func(string, []string) (Result, error) {
	return func(bin string, args []string) (Result, error) {
		key := strings.TrimSpace(bin + " " + strings.Join(args, " "))
		out, ok := outputs[key]
		if !ok {
			return Result{}, fmt.Errorf("run %s: %w", bin, ErrNotFound)
		}
		return Result{Stdout: []byte(out)}, nil
	}
}
