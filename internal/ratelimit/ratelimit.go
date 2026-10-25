package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	windowStart time.Time
	count       int
}

// Limiter is a small in-process fixed-window limiter. It is intentionally
// simple: a single binary deployment does not need Redis, and the API is
// additionally protected by authentication.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

func New() *Limiter {
	return &Limiter{buckets: map[string]*bucket{}}
}

// Allow reports whether key may perform another action in the window.
func (l *Limiter) Allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok || now.Sub(b.windowStart) >= window {
		l.buckets[key] = &bucket{windowStart: now, count: 1}
		return true
	}
	if b.count >= limit {
		return false
	}
	b.count++
	return true
}

// Prune drops expired windows; call periodically to bound memory.
func (l *Limiter) Prune(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if b.windowStart.Before(cutoff) {
			delete(l.buckets, k)
		}
	}
}
