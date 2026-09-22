// Package ratelimit provides a concurrency-safe token-bucket limiter keyed by
// arbitrary strings (e.g. API key, or API key + model).
package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter is a keyed token-bucket rate limiter. Each key gets an independent
// bucket that refills at Rate tokens/sec up to Burst.
type Limiter struct {
	rate  float64 // tokens per second
	burst float64
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

// New creates a limiter allowing `rate` requests/sec with the given burst.
// A rate <= 0 disables limiting (Allow always returns true).
func New(rate float64, burst int) *Limiter {
	if burst <= 0 {
		burst = 1
	}
	return &Limiter{rate: rate, burst: float64(burst), now: time.Now, buckets: make(map[string]*bucket)}
}

// Allow reports whether one request for key may proceed now, consuming a token
// if so.
func (l *Limiter) Allow(key string) bool {
	return l.AllowN(key, 1)
}

// AllowN attempts to consume n tokens for key.
func (l *Limiter) AllowN(key string, n float64) bool {
	if l.rate <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens >= n {
		b.tokens -= n
		return true
	}
	return false
}

// Tokens returns the current token count for key (for observability/tests).
func (l *Limiter) Tokens(key string) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b, ok := l.buckets[key]; ok {
		return b.tokens
	}
	return l.burst
}
