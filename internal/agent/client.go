package agent

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// AuditEntry is one row for the audit log.
type AuditEntry struct {
	Time     time.Time
	Actor    Actor
	Action   string
	ArgsJSON string // secrets already redacted
	OK       bool
	Error    string
}

// Auditor records agent actions. It's defined here, where it is used, so the
// agent package doesn't depend on the store (serve wires in an adapter).
type Auditor interface {
	Audit(ctx context.Context, e AuditEntry) error
}

// Client talks to the agent socket. It is safe for concurrent use.
type Client struct {
	SocketPath string
	Auditor    Auditor // optional
}

// DefaultCallTimeout applies when ctx has no deadline.
const DefaultCallTimeout = 60 * time.Second

// Call runs action with params and decodes the result into out (which may be nil).
// Every non-read-only action is written to the audit log, success or failure.
func (c *Client) Call(ctx context.Context, action string, actor Actor, params, out any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCallTimeout)
		defer cancel()
	}

	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("encode params for %s: %w", action, err)
		}
		raw = b
	}
	req := Request{ID: newID(), Action: action, Actor: actor, Params: raw}

	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		// We don't know if a mutating action ran; audit the attempt anyway.
		c.audit(ctx, req, false, false, err.Error())
		return fmt.Errorf("agent %s: %w", action, err)
	}
	if !resp.OK {
		c.audit(ctx, req, resp.ReadOnly, false, resp.Error.Message)
		return resp.Error
	}
	c.audit(ctx, req, resp.ReadOnly, true, "")
	if out != nil && len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return fmt.Errorf("decode %s result: %w", action, err)
		}
	}
	return nil
}

func (c *Client) roundTrip(ctx context.Context, req Request) (Response, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return Response{}, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}

	b, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	if len(b) > MaxMessageSize {
		return Response{}, Errorf(CodeTooLarge, "request exceeds %d bytes", MaxMessageSize)
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return Response{}, fmt.Errorf("write: %w", err)
	}

	br := bufio.NewReader(io.LimitReader(conn, 16*MaxMessageSize))
	line, err := br.ReadBytes('\n')
	if err != nil && !(errors.Is(err, io.EOF) && len(line) > 0) {
		return Response{}, fmt.Errorf("read: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}
	if resp.ID != req.ID {
		return Response{}, fmt.Errorf("response id mismatch")
	}
	if !resp.OK && resp.Error == nil {
		resp.Error = Errorf(CodeInternal, "agent returned failure without error")
	}
	return resp, nil
}

func (c *Client) audit(ctx context.Context, req Request, readOnly, ok bool, errMsg string) {
	if c.Auditor == nil || readOnly {
		return
	}
	// Use a fresh short context: the request ctx may already be cancelled, and
	// the audit row must still be written.
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	c.Auditor.Audit(actx, AuditEntry{
		Time:     time.Now(),
		Actor:    req.Actor,
		Action:   req.Action,
		ArgsJSON: RedactJSON(req.Params),
		OK:       ok,
		Error:    errMsg,
	})
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
