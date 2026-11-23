package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/slightgrey/layte-panel/internal/config"
	"github.com/slightgrey/layte-panel/internal/store"
)

const (
	cookieName  = "layte_session"
	csrfHeader  = "X-CSRF-Token"
	pendingTTL  = 5 * time.Minute // time allowed between password and TOTP step
	touchEvery  = time.Minute     // don't write last_seen_at on every request
	maxFailures = 5
	lockFor     = 15 * time.Minute
)

// ctxKey is a private type, so no other package can read or overwrite our
// context values by accident (context keys are compared by type and value).
type ctxKey int

const sessionKey ctxKey = 0

type sessionInfo struct {
	Session store.Session
	User    store.User
}

// sessionFrom returns the current request's session, if any.
func sessionFrom(ctx context.Context) (sessionInfo, bool) {
	si, ok := ctx.Value(sessionKey).(sessionInfo)
	return si, ok
}

// loadSession looks up the session cookie on /api requests and attaches the
// session to the request context. Expired sessions are deleted. It never
// rejects a request itself; requireAuth & co. do that.
func (s *server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || !strings.HasPrefix(r.URL.Path, "/api/") || len(c.Value) != 64 {
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()
		hash := hashToken(c.Value)
		ss, err := s.Store.SessionByHash(ctx, hash)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				s.Log.Error("load session", "err", err)
			}
			next.ServeHTTP(w, r)
			return
		}
		now := s.Now()
		if s.sessionExpired(ss, now) {
			s.Store.DeleteSession(ctx, hash)
			next.ServeHTTP(w, r)
			return
		}
		u, err := s.Store.UserByID(ctx, ss.UserID)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if now.Sub(ss.LastSeenAt) > touchEvery {
			s.Store.TouchSession(ctx, hash, now)
		}
		// context.WithValue returns a child context carrying the session;
		// r.WithContext makes a copy of the request that uses it.
		ctx = context.WithValue(ctx, sessionKey, sessionInfo{Session: ss, User: u})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// "Remember this browser for 30 days" (ticked on the 2FA step): the session
// lasts up to rememberMax and ends after rememberIdle unused; its cookie is
// persistent, so it survives closing the browser. Log out, a password change
// or a 2FA reset end it at once, like any session.
const (
	rememberMax  = 30 * 24 * time.Hour
	rememberIdle = 7 * 24 * time.Hour
)

// sessionLimits are the idle and maximum ages that apply to a session.
func (s *server) sessionLimits(remember bool) (idle, max time.Duration) {
	if remember {
		return rememberIdle, rememberMax
	}
	return s.Config.SessionIdle.Duration, s.Config.SessionMax.Duration
}

func (s *server) sessionExpired(ss store.Session, now time.Time) bool {
	if ss.TOTPPending {
		return now.Sub(ss.CreatedAt) > pendingTTL
	}
	idle, max := s.sessionLimits(ss.Remember)
	return now.Sub(ss.LastSeenAt) > idle || now.Sub(ss.CreatedAt) > max
}

// requireAuth allows only fully authenticated sessions (password + TOTP).
func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		si, ok := sessionFrom(r.Context())
		_, isKey := keyFrom(r.Context())
		switch {
		case isKey:
			writeError(w, http.StatusForbidden, "session_required", "this endpoint can't be used with an API key")
		case !ok:
			writeError(w, http.StatusUnauthorized, "unauthenticated", "login required")
		case si.Session.TOTPPending:
			writeError(w, http.StatusUnauthorized, "totp_required", "two-factor code required")
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// requirePending allows only sessions waiting for the TOTP step.
func (s *server) requirePending(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		si, ok := sessionFrom(r.Context())
		if !ok || !si.Session.TOTPPending {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "login first")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAnySession allows pending or full sessions (used by logout).
func (s *server) requireAnySession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := sessionFrom(r.Context()); !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "login required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// csrf protects state-changing requests. With a session, the X-CSRF-Token
// header must match the session's token. Without one (login), the Origin
// header, if sent, must be our own origin. SameSite=Strict cookies add a
// third layer.
func (s *server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !s.originAllowed(r) {
			writeError(w, http.StatusForbidden, "csrf", "cross-origin request rejected")
			return
		}
		// Login replaces whatever session exists, so it's exempt from the
		// session token check (the Origin check above still applies).
		if si, ok := sessionFrom(r.Context()); ok && r.URL.Path != "/api/v1/auth/login" {
			got := r.Header.Get(csrfHeader)
			if subtle.ConstantTimeCompare([]byte(got), []byte(si.Session.CSRFToken)) != 1 {
				writeError(w, http.StatusForbidden, "csrf", "missing or invalid CSRF token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client; browsers always send Origin on cross-site POSTs
	}
	if s.Config.Dev && origin == s.Config.DevOrigin {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

// rateLimited applies the per-IP auth limiter.
func (s *server) rateLimited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authLimiter.Allow(clientIP(r, s.Config.TrustedProxies)) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again in a minute")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// devCORS lets the Vite dev server call the API directly. Off in production.
func (s *server) devCORS(next http.Handler) http.Handler {
	if !s.Config.Dev {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == s.Config.DevOrigin {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", s.Config.DevOrigin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Headers", "Content-Type, "+csrfHeader)
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the caller's IP. X-Forwarded-For is only trusted when the
// direct peer is a trusted proxy (our nginx); then the rightmost untrusted
// address is the real client (nginx appends it with $proxy_add_x_forwarded_for).
func clientIP(r *http.Request, trusted []string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !slices.Contains(trusted, host) {
		return host
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" || slices.Contains(trusted, ip) {
			continue
		}
		if net.ParseIP(ip) != nil {
			return ip
		}
		break
	}
	return host
}

// setSessionCookie sets the session cookie. maxAge 0 = a browser-session
// cookie (gone when the browser closes); remembered sessions pass their
// lifetime so the cookie survives restarts.
func (s *server) setSessionCookie(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   !s.Config.Dev,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   !s.Config.Dev,
		SameSite: http.SameSiteStrictMode,
	})
}

// CleanupSessions deletes expired sessions every interval until ctx is cancelled.
// Run it in its own goroutine: `go api.CleanupSessions(ctx, ...)`.
func CleanupSessions(ctx context.Context, st *store.Store, cfg config.Config, log *slog.Logger, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now()
			n, err := st.DeleteExpiredSessions(ctx,
				store.SessionLimits{IdleBefore: now.Add(-cfg.SessionIdle.Duration), CreatedBefore: now.Add(-cfg.SessionMax.Duration)},
				store.SessionLimits{IdleBefore: now.Add(-rememberIdle), CreatedBefore: now.Add(-rememberMax)},
				now.Add(-pendingTTL))
			if err != nil {
				log.Error("session cleanup", "err", err)
			} else if n > 0 {
				log.Debug("session cleanup", "deleted", n)
			}
		}
	}
}
