package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"
)

// Action is one entry in the fixed allowlist of things the agent can do.
// There is deliberately no generic "run command" action.
type Action struct {
	Name     string
	ReadOnly bool
	Timeout  time.Duration
	handle   func(ctx context.Context, actor Actor, params json.RawMessage) (any, error)
}

// Validator is implemented by every params struct.
type Validator interface {
	Validate() error
}

// NoParams is the params type for actions that take no input.
type NoParams struct{}

func (NoParams) Validate() error { return nil }

// Def describes an action whose params decode into P.
//
// `[P Validator]` is a type parameter (generics): Def works with any params
// struct type P that has a Validate method. The compiler checks this, so each
// handler receives its own typed, already-validated struct instead of raw JSON.
func Def[P Validator](name string, readOnly bool, timeout time.Duration,
	fn func(ctx context.Context, actor Actor, p P) (any, error)) Action {
	return Action{
		Name:     name,
		ReadOnly: readOnly,
		Timeout:  timeout,
		handle: func(ctx context.Context, actor Actor, raw json.RawMessage) (any, error) {
			var p P
			if len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&p); err != nil {
					return nil, Errorf(CodeInvalidParams, "decode params: %v", err)
				}
			}
			if err := p.Validate(); err != nil {
				return nil, Errorf(CodeInvalidParams, "%v", err)
			}
			return fn(ctx, actor, p)
		},
	}
}

var actionName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// Registry maps action names to actions. It's built once at startup and then
// only read, so it needs no locking.
type Registry struct {
	actions map[string]Action
}

func NewRegistry() *Registry { return &Registry{actions: map[string]Action{}} }

// Register adds actions. It panics on programmer mistakes (bad or duplicate
// names) because those can only happen at startup, never in a request path.
func (r *Registry) Register(actions ...Action) {
	for _, a := range actions {
		if !actionName.MatchString(a.Name) {
			panic(fmt.Sprintf("agent: invalid action name %q", a.Name))
		}
		if _, dup := r.actions[a.Name]; dup {
			panic(fmt.Sprintf("agent: duplicate action %q", a.Name))
		}
		if a.Timeout <= 0 {
			a.Timeout = 30 * time.Second
		}
		r.actions[a.Name] = a
	}
}

func (r *Registry) Lookup(name string) (Action, bool) {
	a, ok := r.actions[name]
	return a, ok
}

// Names lists registered actions (sorted), for logging at startup.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.actions))
	for n := range r.actions {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
