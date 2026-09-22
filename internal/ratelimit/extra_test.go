package ratelimit

import (
	"testing"
	"time"
)

func TestAllowNConsumesMultiple(t *testing.T) {
	base := time.Now()
	l := New(100, 10)
	l.now = func() time.Time { return base }
	if !l.AllowN("k", 5) {
		t.Fatal("should allow 5 from burst of 10")
	}
	if !l.AllowN("k", 5) {
		t.Fatal("should allow another 5")
	}
	if l.AllowN("k", 5) {
		t.Fatal("bucket should be empty now")
	}
}

func TestTokensReportsRemaining(t *testing.T) {
	base := time.Now()
	l := New(10, 4)
	l.now = func() time.Time { return base }
	if got := l.Tokens("fresh"); got != 4 {
		t.Fatalf("fresh key tokens = %v, want burst 4", got)
	}
	l.Allow("k")
	if got := l.Tokens("k"); got < 2.9 || got > 3.1 {
		t.Fatalf("tokens after one allow = %v, want ~3", got)
	}
}
