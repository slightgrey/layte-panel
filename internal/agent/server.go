package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// Server listens on the agent socket and dispatches requests to the registry.
type Server struct {
	Registry   *Registry
	Log        *slog.Logger
	SocketPath string
	Group      string // group allowed to connect ("layte"); ignored when Dev
	Dev        bool
}

// readTimeout bounds how long a client may take to send its request line.
const readTimeout = 5 * time.Second

// Serve listens until ctx is cancelled, then waits for in-flight requests.
func (s *Server) Serve(ctx context.Context) error {
	if err := s.prepareSocketPath(); err != nil {
		return err
	}
	ln, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.SocketPath, err)
	}
	defer os.Remove(s.SocketPath)

	gid, err := s.secureSocket()
	if err != nil {
		ln.Close()
		return err
	}

	s.Log.Info("layte agent listening", "socket", s.SocketPath, "actions", s.Registry.Names(), "dev", s.Dev)

	// Closing the listener makes Accept return, ending the loop below.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	// A WaitGroup counts running goroutines so shutdown can wait for them.
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.Log.Info("agent shutting down")
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return fmt.Errorf("accept: %w", err)
		}
		// Each connection is handled in its own goroutine: a function running
		// concurrently with the accept loop. Goroutines are cheap (a few KB),
		// so one per request is normal in Go.
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handleConn(ctx, conn, gid)
		}()
	}
}

// prepareSocketPath creates the socket directory and removes a stale socket.
func (s *Server) prepareSocketPath() error {
	if err := os.MkdirAll(filepath.Dir(s.SocketPath), 0o750); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	fi, err := os.Lstat(s.SocketPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// In production only ever delete an actual socket, never a regular file
	// someone put there. (On Windows dev, sockets don't report ModeSocket.)
	if fi.Mode()&fs.ModeSocket == 0 && !s.Dev {
		return fmt.Errorf("%s exists and is not a socket; refusing to remove it", s.SocketPath)
	}
	return os.Remove(s.SocketPath)
}

// secureSocket sets mode 0660 and group ownership, returning the allowed gid
// (-1 in dev or on non-Linux systems).
func (s *Server) secureSocket() (int, error) {
	if s.Dev || runtime.GOOS != "linux" {
		return -1, nil
	}
	g, err := user.LookupGroup(s.Group)
	if err != nil {
		return -1, fmt.Errorf("look up group %q: %w", s.Group, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return -1, fmt.Errorf("group %q gid: %w", s.Group, err)
	}
	if err := os.Chown(s.SocketPath, 0, gid); err != nil {
		return -1, fmt.Errorf("chown socket: %w", err)
	}
	if err := os.Chmod(s.SocketPath, 0o660); err != nil {
		return -1, fmt.Errorf("chmod socket: %w", err)
	}
	return gid, nil
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn, gid int) {
	defer conn.Close()

	if gid >= 0 {
		if err := checkPeer(conn, gid); err != nil {
			s.Log.Warn("rejected agent connection", "err", err)
			return
		}
	}

	conn.SetReadDeadline(time.Now().Add(readTimeout))
	req, rerr := readRequest(conn)
	resp := s.dispatch(ctx, req, rerr)

	b, err := json.Marshal(resp)
	if err != nil {
		b, _ = json.Marshal(Response{ID: req.ID, Error: Errorf(CodeInternal, "encode response")})
	}
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	conn.Write(append(b, '\n'))
}

// readRequest reads exactly one '\n'-terminated JSON line, at most MaxMessageSize bytes.
func readRequest(r io.Reader) (Request, *Error) {
	br := bufio.NewReader(io.LimitReader(r, MaxMessageSize+1))
	line, err := br.ReadBytes('\n')
	if len(line) > MaxMessageSize {
		return Request{}, Errorf(CodeTooLarge, "request exceeds %d bytes", MaxMessageSize)
	}
	if err != nil && !(errors.Is(err, io.EOF) && len(line) > 0) {
		return Request{}, Errorf(CodeBadRequest, "read request: %v", err)
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Request{}, Errorf(CodeBadRequest, "decode request: %v", err)
	}
	return req, nil
}

func (s *Server) dispatch(ctx context.Context, req Request, rerr *Error) Response {
	if rerr != nil {
		return Response{ID: req.ID, Error: rerr}
	}
	log := s.Log.With("id", req.ID, "action", req.Action, "user", req.Actor.Username)

	action, ok := s.Registry.Lookup(req.Action)
	if !ok {
		log.Warn("unknown action")
		return Response{ID: req.ID, Error: Errorf(CodeUnknownAction, "unknown action %q", req.Action)}
	}

	// WithTimeout derives a child context that is cancelled automatically after
	// the action's timeout. exec.CommandContext kills the process when it fires.
	actx, cancel := context.WithTimeout(ctx, action.Timeout)
	defer cancel()

	start := time.Now()
	data, err := s.safeHandle(actx, action, req)
	resp := Response{ID: req.ID, ReadOnly: action.ReadOnly}
	if err != nil {
		resp.Error = toAgentError(actx, err)
		log.Warn("action failed", "dur", time.Since(start), "code", resp.Error.Code, "err", err)
		return resp
	}
	b, err := json.Marshal(data)
	if err != nil {
		resp.Error = Errorf(CodeInternal, "encode result: %v", err)
		return resp
	}
	resp.OK, resp.Data = true, b
	if action.Quiet {
		log.Debug("action ok", "dur", time.Since(start))
	} else {
		log.Info("action ok", "dur", time.Since(start))
	}
	return resp
}

// safeHandle converts a panic inside an action into an error so one bad
// action can't crash the agent.
func (s *Server) safeHandle(ctx context.Context, a Action, req Request) (data any, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = Errorf(CodeInternal, "action panicked: %v", v)
		}
	}()
	return a.handle(ctx, req.Actor, req.Params)
}

func toAgentError(ctx context.Context, err error) *Error {
	var ae *Error
	if errors.As(err, &ae) {
		return ae
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Errorf(CodeTimeout, "action timed out")
	}
	return Errorf(CodeInternal, "%v", err)
}
