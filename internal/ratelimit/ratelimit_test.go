package ratelimit

import (
	"testing"
	"time"
)

func TestTokenBucketBurstThenRefill(t *testing.T) {
	base := time.Now()
	l := New(10, 2) // 10 rps, burst 2
	l.now = func() time.Time { return base }

	if !l.Allow("k") || !l.Allow("k") {
		t.Fatal("burst of 2 should be allowed")
	}
	if l.Allow("k") {
		t.Fatal("third immediate request should be blocked")
	}
	// After 100ms at 10 rps, one token has refilled.
	l.now = func() time.Time { return base.Add(100 * time.Millisecond) }
	if !l.Allow("k") {
		t.Fatal("expected one refilled token")
	}
}

func TestRateLimitKeysAreIndependent(t *testing.T) {
	l := New(1, 1)
	if !l.Allow("a") {
		t.Fatal("a should pass")
	}
	if !l.Allow("b") {
		t.Fatal("b has its own bucket and should pass")
	}
}

func TestRateLimitDisabled(t *testing.T) {
	l := New(0, 1)
	for i := 0; i < 100; i++ {
		if !l.Allow("k") {
			t.Fatal("rate=0 means unlimited")
		}
	}
}
