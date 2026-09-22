package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBreakerTripsAndRecovers(t *testing.T) {
	now := time.Now()
	b := NewBreaker(3, 100*time.Millisecond)
	b.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("call %d should be allowed while closed", i)
		}
		b.Failure()
	}
	if b.State() != Open {
		t.Fatalf("state = %v, want open", b.State())
	}
	if b.Allow() {
		t.Fatal("open breaker should reject calls")
	}

	// Advance past cooldown -> half-open allows a single probe.
	now = now.Add(150 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("half-open should allow one probe")
	}
	if b.Allow() {
		t.Fatal("half-open should allow only one concurrent probe")
	}
	b.Success()
	if b.State() != Closed {
		t.Fatalf("state after successful probe = %v, want closed", b.State())
	}
}

func TestRetrySucceedsAfterTransientErrors(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}
	attempts := 0
	err := p.Retry(context.Background(), func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryStopsOnContextCancel(t *testing.T) {
	p := DefaultRetry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.Retry(ctx, func(context.Context) error { return errors.New("x") })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRetryDoesNotRetryNonRetryable(t *testing.T) {
	sentinel := errors.New("fatal")
	p := RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, Retryable: func(error) bool { return false }}
	attempts := 0
	err := p.Retry(context.Background(), func(context.Context) error {
		attempts++
		return sentinel
	})
	if !errors.Is(err, sentinel) || attempts != 1 {
		t.Fatalf("attempts = %d err = %v", attempts, err)
	}
}
