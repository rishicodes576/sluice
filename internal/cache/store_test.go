package cache

import (
	"testing"
	"time"
)

func TestMemoryTTLExpiry(t *testing.T) {
	m := NewMemory(4, 50*time.Millisecond)
	base := time.Now()
	m.now = func() time.Time { return base }
	m.Set("k", &Entry{Response: []byte("v")})
	if _, ok := m.Get("k"); !ok {
		t.Fatal("expected fresh entry")
	}
	m.now = func() time.Time { return base.Add(100 * time.Millisecond) }
	if _, ok := m.Get("k"); ok {
		t.Fatal("expected expiry after TTL")
	}
}

func TestMemoryPurgeAndLen(t *testing.T) {
	m := NewMemory(8, 0)
	for i := 0; i < 10; i++ {
		m.Set(itoaCache(i), &Entry{Response: []byte("v")})
	}
	if m.Len() != 10 {
		t.Fatalf("len = %d, want 10", m.Len())
	}
	m.Purge()
	if m.Len() != 0 {
		t.Fatalf("len after purge = %d, want 0", m.Len())
	}
}

func TestShardCountRoundsToPowerOfTwo(t *testing.T) {
	m := NewMemory(5, 0)
	if len(m.shards) != 8 {
		t.Fatalf("shards = %d, want 8", len(m.shards))
	}
}

func itoaCache(i int) string {
	return string(rune('a'+i%26)) + string(rune('0'+i/26))
}
