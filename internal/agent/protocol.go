// Package agent implements the privileged helper (`layte agent`) and the
// client that `layte serve` uses to talk to it.
//
// Wire protocol: one request per Unix-socket connection. The client writes one
// line of JSON (a Request) terminated by '\n'; the agent replies with one line
// (a Response) and closes the connection.
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
)

// MaxMessageSize caps a request or response line.
const MaxMessageSize = 1 << 20 // 1 MiB

// Actor identifies the panel user on whose behalf an action runs (for logs/audit).
type Actor struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
}

// Request is what serve sends to the agent.
type Request struct {
	ID     string          `json:"id"`
	Action string          `json:"action"`
	Actor  Actor           `json:"actor"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is what the agent sends back.
type Response struct {
	ID       string          `json:"id"`
	OK       bool            `json:"ok"`
	ReadOnly bool            `json:"read_only"` // lets the client decide whether to audit
	Data     json.RawMessage `json:"data,omitempty"`
	Error    *Error          `json:"error,omitempty"`
}

// Error codes returned by the agent.
const (
	CodeUnknownAction = "unknown_action"
	CodeInvalidParams = "invalid_params"
	CodeTooLarge      = "request_too_large"
	CodeBadRequest    = "bad_request"
	CodeTimeout       = "timeout"
	CodeNotFound      = "not_found"
	CodeInternal      = "internal"
)

// Error is a typed agent error. It implements the built-in `error` interface
// (any type with an `Error() string` method does).
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("agent: %s: %s", e.Code, e.Message) }

// Errorf builds a typed error for handlers to return.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// IsCode reports whether err (or anything it wraps) is an agent *Error with code.
// errors.As walks the chain created by fmt.Errorf("...: %w", err).
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}
