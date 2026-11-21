package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// LocalClient calls actions in-process, without a socket. It behaves like
// Client (typed params, validation, timeouts, audit) and is used by tests
// that exercise the API together with the real actions on a fake root.
type LocalClient struct {
	Registry *Registry
	Auditor  Auditor
}

func (c *LocalClient) Call(ctx context.Context, action string, actor Actor, params, out any) error {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	a, ok := c.Registry.Lookup(action)
	if !ok {
		return Errorf(CodeUnknownAction, "unknown action %q", action)
	}
	actx, cancel := context.WithTimeout(ctx, a.Timeout)
	defer cancel()
	data, err := a.handle(actx, actor, raw)
	if c.Auditor != nil && a.audited(raw) {
		e := AuditEntry{Time: time.Now(), Actor: actor, Action: action, ArgsJSON: RedactJSON(raw), OK: err == nil}
		if err != nil {
			e.Error = err.Error()
		}
		c.Auditor.Audit(context.WithoutCancel(ctx), e)
	}
	if err != nil {
		return toAgentError(actx, err)
	}
	if out == nil {
		return nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode %s result: %w", action, err)
	}
	return json.Unmarshal(b, out)
}
