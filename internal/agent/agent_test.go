package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type echoParams struct {
	Name     string `json:"name"`
	Password string `json:"password,omitempty"`
}

func (p echoParams) Validate() error {
	if p.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

type memAuditor struct {
	mu      sync.Mutex
	entries []AuditEntry
}

func (m *memAuditor) Audit(_ context.Context, e AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return nil
}

// startAgent runs a Server on a temp socket and returns a Client for it.
func startAgent(t *testing.T) (*Client, *memAuditor) {
	t.Helper()
	reg := NewRegistry()
	reg.Register(
		Def("test.echo", false, time.Second, func(_ context.Context, a Actor, p echoParams) (any, error) {
			return map[string]string{"hello": p.Name, "by": a.Username}, nil
		}),
		Def("test.info", true, time.Second, func(context.Context, Actor, NoParams) (any, error) {
			return map[string]int{"n": 42}, nil
		}),
		Def("test.slow", true, 50*time.Millisecond, func(ctx context.Context, _ Actor, _ NoParams) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}),
		Def("test.panic", true, time.Second, func(context.Context, Actor, NoParams) (any, error) {
			panic("boom")
		}),
	)
	dir, err := os.MkdirTemp("", "lagent") // short path: unix socket paths are length-limited
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "a.sock")
	srv := &Server{Registry: reg, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), SocketPath: sock, Dev: true}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
		os.RemoveAll(dir)
	})

	// Wait for the socket to appear.
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	aud := &memAuditor{}
	return &Client{SocketPath: sock, Auditor: aud}, aud
}

func TestRoundTrip(t *testing.T) {
	c, aud := startAgent(t)
	ctx := context.Background()
	actor := Actor{UserID: 1, Username: "admin"}

	var out map[string]string
	if err := c.Call(ctx, "test.echo", actor, echoParams{Name: "world", Password: "hunter2"}, &out); err != nil {
		t.Fatal(err)
	}
	if out["hello"] != "world" || out["by"] != "admin" {
		t.Errorf("got %v", out)
	}

	var info map[string]int
	if err := c.Call(ctx, "test.info", actor, nil, &info); err != nil || info["n"] != 42 {
		t.Fatalf("info: %v %v", info, err)
	}

	// Only the mutating action is audited, with the password redacted.
	if len(aud.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(aud.entries))
	}
	e := aud.entries[0]
	if e.Action != "test.echo" || !e.OK || strings.Contains(e.ArgsJSON, "hunter2") || !strings.Contains(e.ArgsJSON, "[redacted]") {
		t.Errorf("bad audit entry: %+v", e)
	}
}

func TestErrors(t *testing.T) {
	c, _ := startAgent(t)
	ctx := context.Background()
	tests := []struct {
		name   string
		action string
		params any
		code   string
	}{
		{"unknown action", "system.rm_rf", nil, CodeUnknownAction},
		{"validation", "test.echo", echoParams{}, CodeInvalidParams},
		{"unknown field", "test.echo", map[string]string{"name": "x", "cmd": "id"}, CodeInvalidParams},
		{"timeout", "test.slow", nil, CodeTimeout},
		{"panic", "test.panic", nil, CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Call(ctx, tt.action, Actor{}, tt.params, nil)
			if !IsCode(err, tt.code) {
				t.Errorf("err = %v, want code %s", err, tt.code)
			}
		})
	}
}

func TestOversizedRequest(t *testing.T) {
	c, _ := startAgent(t)
	conn, err := net.Dial("unix", c.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Write more than MaxMessageSize without a newline; the agent must reject it.
	go conn.Write([]byte(strings.Repeat("a", MaxMessageSize+10)))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	b, _ := io.ReadAll(conn)
	var resp Response
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("decode %q: %v", b, err)
	}
	if resp.Error == nil || resp.Error.Code != CodeTooLarge {
		t.Errorf("got %+v", resp)
	}
}

func TestRegisterRejectsBadNames(t *testing.T) {
	for _, name := range []string{"exec", "Shell.Run", "a..b", "site.create;rm"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%q) did not panic", name)
				}
			}()
			NewRegistry().Register(Def(name, true, 0, func(context.Context, Actor, NoParams) (any, error) { return nil, nil }))
		}()
	}
}

func TestRedactJSON(t *testing.T) {
	got := RedactJSON(json.RawMessage(`{"user":"a","db_password":"x","nested":{"api_token":"y","list":[{"secret":"z"}]}}`))
	for _, leak := range []string{`"x"`, `"y"`, `"z"`} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %s in %s", leak, got)
		}
	}
	if !strings.Contains(got, `"user":"a"`) {
		t.Errorf("non-secret removed: %s", got)
	}
}
