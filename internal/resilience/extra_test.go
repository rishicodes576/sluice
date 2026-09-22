package resilience

import (
	"errors"
	"testing"
	"time"
)

func TestBreakerDoWrapsSuccessAndFailure(t *testing.T) {
	b := NewBreaker(2, time.Second)
	if err := b.Do(func() error { return nil }); err != nil {
		t.Fatalf("success Do returned %v", err)
	}
	boom := errors.New("boom")
	if err := b.Do(func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("failure Do returned %v", err)
	}
}

func TestBreakerDoRejectsWhenOpen(t *testing.T) {
	b := NewBreaker(1, time.Hour)
	_ = b.Do(func() error { return errors.New("x") }) // trips (threshold 1)
	if err := b.Do(func() error { return nil }); !errors.Is(err, ErrOpen) {
		t.Fatalf("expected ErrOpen, got %v", err)
	}
}

func TestHalfOpenFailureReopens(t *testing.T) {
	now := time.Now()
	b := NewBreaker(1, 10*time.Millisecond)
	b.now = func() time.Time { return now }
	b.Failure() // open
	now = now.Add(20 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("expected half-open probe to be allowed")
	}
	b.Failure() // half-open failure -> reopen
	if b.State() != Open {
		t.Fatalf("state = %v, want open", b.State())
	}
}

func TestStateString(t *testing.T) {
	for s, want := range map[State]string{Closed: "closed", Open: "open", HalfOpen: "half-open"} {
		if s.String() != want {
			t.Errorf("State(%d).String() = %q, want %q", s, s.String(), want)
		}
	}
}

func TestDefaultRetryValues(t *testing.T) {
	p := DefaultRetry()
	if p.MaxAttempts != 3 || p.BaseDelay <= 0 {
		t.Fatalf("unexpected defaults %+v", p)
	}
}
