// Package resilience provides a circuit breaker and a retry helper with
// exponential backoff and full jitter, used to guard upstream provider calls.
package resilience

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

// State is the circuit breaker state.
type State int

const (
	// Closed allows requests through and counts failures.
	Closed State = iota
	// Open rejects requests immediately until the cooldown elapses.
	Open
	// HalfOpen allows a limited probe to test recovery.
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// ErrOpen is returned when the breaker is open and rejects a call.
var ErrOpen = errors.New("resilience: circuit breaker is open")

// Breaker is a concurrency-safe circuit breaker. It trips to Open after
// FailureThreshold consecutive failures and, after Cooldown, allows a single
// HalfOpen probe before closing (on success) or re-opening (on failure).
type Breaker struct {
	failureThreshold int
	cooldown         time.Duration
	now              func() time.Time

	mu           sync.Mutex
	state        State
	consecFails  int
	openedAt     time.Time
	halfOpenBusy bool
}

// NewBreaker constructs a Breaker. A threshold <= 0 defaults to 5 and a
// cooldown <= 0 defaults to 10s.
func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = 10 * time.Second
	}
	return &Breaker{failureThreshold: threshold, cooldown: cooldown, now: time.Now}
}

// State returns the current state, transitioning Open->HalfOpen if the cooldown
// has elapsed.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maybeHalfOpen()
	return b.state
}

func (b *Breaker) maybeHalfOpen() {
	if b.state == Open && b.now().Sub(b.openedAt) >= b.cooldown {
		b.state = HalfOpen
		b.halfOpenBusy = false
	}
}

// Allow reports whether a call may proceed right now.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maybeHalfOpen()
	switch b.state {
	case Open:
		return false
	case HalfOpen:
		if b.halfOpenBusy {
			return false
		}
		b.halfOpenBusy = true
		return true
	default:
		return true
	}
}

// Success records a successful call, closing the breaker.
func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecFails = 0
	b.state = Closed
	b.halfOpenBusy = false
}

// Failure records a failed call, potentially tripping the breaker.
func (b *Breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecFails++
	if b.state == HalfOpen || b.consecFails >= b.failureThreshold {
		b.state = Open
		b.openedAt = b.now()
		b.halfOpenBusy = false
	}
}

// Do runs fn guarded by the breaker.
func (b *Breaker) Do(fn func() error) error {
	if !b.Allow() {
		return ErrOpen
	}
	if err := fn(); err != nil {
		b.Failure()
		return err
	}
	b.Success()
	return nil
}

// RetryPolicy configures exponential backoff with full jitter.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	// Retryable decides whether an error is worth retrying. If nil, all errors
	// except context cancellation are retried.
	Retryable func(error) bool
}

// DefaultRetry returns a sensible default policy.
func DefaultRetry() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseDelay: 50 * time.Millisecond, MaxDelay: 2 * time.Second}
}

func (p RetryPolicy) retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if p.Retryable != nil {
		return p.Retryable(err)
	}
	return true
}

// backoff computes the full-jitter delay for a given zero-based attempt.
func (p RetryPolicy) backoff(attempt int, rng *rand.Rand) time.Duration {
	exp := float64(p.BaseDelay) * math.Pow(2, float64(attempt))
	if p.MaxDelay > 0 && exp > float64(p.MaxDelay) {
		exp = float64(p.MaxDelay)
	}
	return time.Duration(rng.Int63n(int64(exp) + 1))
}

// Retry invokes fn until it succeeds, the policy is exhausted, the error is not
// retryable, or the context is cancelled. It returns the last error.
func (p RetryPolicy) Retry(ctx context.Context, fn func(ctx context.Context) error) error {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 1
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var last error
	for attempt := 0; attempt < p.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		last = fn(ctx)
		if last == nil {
			return nil
		}
		if !p.retryable(last) || attempt == p.MaxAttempts-1 {
			return last
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.backoff(attempt, rng)):
		}
	}
	return last
}
