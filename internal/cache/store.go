// Package cache implements Sluice's semantic response cache: a pluggable
// key-value store for payloads plus a vector index over prompt embeddings so
// that semantically similar prompts reuse a previous response.
package cache

import (
	"sync"
	"time"
)

// Entry is a cached completion payload.
type Entry struct {
	Response  []byte    // serialised provider.ChatResponse
	Model     string    // model the response was produced for
	CreatedAt time.Time // insertion time (for TTL)
}

// Store persists cache entries by key. Implementations must be concurrency safe.
// The interface is the seam for alternative backends (e.g. Redis) without
// touching the semantic-cache logic.
type Store interface {
	Get(key string) (*Entry, bool)
	Set(key string, e *Entry)
	Delete(key string)
	Len() int
	Purge()
}

// shard is one striped partition of the memory store, with its own lock to
// reduce contention under concurrent load.
type shard struct {
	mu      sync.RWMutex
	entries map[string]*Entry
}

// Memory is a sharded, TTL-aware in-memory store. It stripes keys across
// shards to keep lock contention low at high request rates.
type Memory struct {
	shards []*shard
	mask   uint64
	ttl    time.Duration
	now    func() time.Time
}

// NewMemory creates a memory store with the given number of shards (rounded up
// to a power of two) and TTL (0 disables expiry).
func NewMemory(shardCount int, ttl time.Duration) *Memory {
	n := 1
	for n < shardCount {
		n <<= 1
	}
	if n < 1 {
		n = 1
	}
	m := &Memory{shards: make([]*shard, n), mask: uint64(n - 1), ttl: ttl, now: time.Now}
	for i := range m.shards {
		m.shards[i] = &shard{entries: make(map[string]*Entry)}
	}
	return m
}

// fnv64 hashes a key to select its shard.
func fnv64(s string) uint64 {
	const (
		offset = 1469598103934665603
		prime  = 1099511628211
	)
	h := uint64(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}

func (m *Memory) shardFor(key string) *shard { return m.shards[fnv64(key)&m.mask] }

// Get implements Store, honouring TTL by lazily evicting expired entries.
func (m *Memory) Get(key string) (*Entry, bool) {
	sh := m.shardFor(key)
	sh.mu.RLock()
	e, ok := sh.entries[key]
	sh.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if m.ttl > 0 && m.now().Sub(e.CreatedAt) > m.ttl {
		sh.mu.Lock()
		delete(sh.entries, key)
		sh.mu.Unlock()
		return nil, false
	}
	return e, true
}

// Set implements Store.
func (m *Memory) Set(key string, e *Entry) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = m.now()
	}
	sh := m.shardFor(key)
	sh.mu.Lock()
	sh.entries[key] = e
	sh.mu.Unlock()
}

// Delete implements Store.
func (m *Memory) Delete(key string) {
	sh := m.shardFor(key)
	sh.mu.Lock()
	delete(sh.entries, key)
	sh.mu.Unlock()
}

// Len implements Store.
func (m *Memory) Len() int {
	n := 0
	for _, sh := range m.shards {
		sh.mu.RLock()
		n += len(sh.entries)
		sh.mu.RUnlock()
	}
	return n
}

// Purge implements Store, clearing all entries.
func (m *Memory) Purge() {
	for _, sh := range m.shards {
		sh.mu.Lock()
		sh.entries = make(map[string]*Entry)
		sh.mu.Unlock()
	}
}
