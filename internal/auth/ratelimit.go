package auth

import (
	"sync"
	"time"
)

// RateLimiter is an in-memory token bucket per key (e.g. client IP).
// Each key gets `burst` tokens, refilled at `perMinute` tokens per minute.
type RateLimiter struct {
	perMinute float64
	burst     float64
	now       func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(perMinute, burst int) *RateLimiter {
	return &RateLimiter{
		perMinute: float64(perMinute),
		burst:     float64(burst),
		now:       time.Now,
		buckets:   map[string]*bucket{},
	}
}

// Allow takes one token for key, reporting false if none are left.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
		if len(l.buckets) > 10000 {
			l.prune(now)
		}
	}
	b.tokens += now.Sub(b.last).Minutes() * l.perMinute
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// prune drops buckets that have fully refilled (they behave like new ones),
// so memory can't grow without bound. Caller holds l.mu.
func (l *RateLimiter) prune(now time.Time) {
	full := time.Duration(l.burst / l.perMinute * float64(time.Minute))
	for k, b := range l.buckets {
		if now.Sub(b.last) > full {
			delete(l.buckets, k)
		}
	}
}
