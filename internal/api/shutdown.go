package api

import "context"

// Live streams (log tail, job progress over SSE) never end on their own, so
// a graceful shutdown would wait for them until its deadline. The server puts
// a "stopping" channel into every request's context (http.Server.BaseContext)
// and closes it when Shutdown starts; streams watch it and end, while normal
// requests still finish. Browsers' EventSource reconnects by itself.

type stoppingKey struct{}

// WithStopping returns a base context whose streams end when stop is closed.
func WithStopping(ctx context.Context, stop <-chan struct{}) context.Context {
	return context.WithValue(ctx, stoppingKey{}, stop)
}

// stopping is closed when the server shuts down (nil, which never fires,
// when the server didn't set one, e.g. in tests).
func stopping(ctx context.Context) <-chan struct{} {
	ch, _ := ctx.Value(stoppingKey{}).(<-chan struct{})
	return ch
}
