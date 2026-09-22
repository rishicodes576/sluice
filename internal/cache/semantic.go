package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"

	"github.com/rishicodes576/sluice/internal/embed"
	"github.com/rishicodes576/sluice/internal/vectorstore"
)

// Semantic is a cache that returns a stored response when an incoming prompt is
// close enough (cosine similarity >= threshold) to a previously seen prompt.
// It combines an embedder, a vector index and a payload store, and tracks hit
// statistics including estimated cost saved.
type Semantic struct {
	embedder  embed.Embedder
	index     vectorstore.Store
	store     Store
	threshold float64
	mu        sync.Mutex // serialises index writes with key derivation

	lookups   atomic.Int64
	hits      atomic.Int64
	tokensCut atomic.Int64
	costSaved atomicFloat
}

// Options configures a Semantic cache.
type Options struct {
	Threshold  float64 // minimum cosine similarity for a hit (e.g. 0.95)
	ShardCount int
}

// New builds a Semantic cache. If index is nil an HNSW index is created.
func New(e embed.Embedder, index vectorstore.Store, store Store, opts Options) *Semantic {
	if opts.Threshold <= 0 {
		opts.Threshold = 0.95
	}
	if index == nil {
		index = vectorstore.NewHNSW(e.Dim())
	}
	return &Semantic{embedder: e, index: index, store: store, threshold: opts.Threshold}
}

// keyOf derives a stable id for a (model, embedding) pair. The model is part of
// the key so responses are never crossed between models.
func keyOf(model string, vec []float64) string {
	h := sha256.New()
	h.Write([]byte(model))
	buf := make([]byte, 8)
	for _, f := range vec {
		bits := float64bits(f)
		for i := 0; i < 8; i++ {
			buf[i] = byte(bits >> (8 * i))
		}
		h.Write(buf)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Lookup returns a cached entry for a prompt if a semantically similar prompt
// for the same model is present above the similarity threshold.
func (s *Semantic) Lookup(ctx context.Context, model, prompt string) (*Entry, bool, error) {
	s.lookups.Add(1)
	vecs, err := s.embedder.Embed(ctx, []string{prompt})
	if err != nil {
		return nil, false, err
	}
	vec := vecs[0]
	matches := s.index.Search(vec, 1)
	if len(matches) == 0 || matches[0].Similarity < s.threshold {
		return nil, false, nil
	}
	e, ok := s.store.Get(matches[0].ID)
	if !ok || e.Model != model {
		return nil, false, nil
	}
	s.hits.Add(1)
	return e, true, nil
}

// Store indexes a prompt and its response for future semantic lookups. cost and
// tokens describe what a fresh upstream call would have cost, used to compute
// the running savings estimate on future hits.
func (s *Semantic) Store(ctx context.Context, model, prompt string, resp []byte, tokens int, cost float64) error {
	vecs, err := s.embedder.Embed(ctx, []string{prompt})
	if err != nil {
		return err
	}
	vec := vecs[0]
	key := keyOf(model, vec)
	s.mu.Lock()
	if err := s.index.Add(key, vec); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	s.store.Set(key, &Entry{Response: resp, Model: model})
	// Attribute the would-be cost of this entry to future savings.
	s.tokensCut.Add(int64(tokens))
	s.costSaved.add(cost)
	return nil
}

// Stats is a snapshot of cache effectiveness.
type Stats struct {
	Lookups   int64   `json:"lookups"`
	Hits      int64   `json:"hits"`
	HitRatio  float64 `json:"hit_ratio"`
	Entries   int     `json:"entries"`
	TokensCut int64   `json:"tokens_saved"`
	CostSaved float64 `json:"cost_saved_usd"`
	Threshold float64 `json:"threshold"`
}

// Stats returns a snapshot of cache statistics.
func (s *Semantic) Stats() Stats {
	l := s.lookups.Load()
	h := s.hits.Load()
	ratio := 0.0
	if l > 0 {
		ratio = float64(h) / float64(l)
	}
	return Stats{
		Lookups: l, Hits: h, HitRatio: ratio, Entries: s.store.Len(),
		TokensCut: s.tokensCut.Load(), CostSaved: s.costSaved.load(), Threshold: s.threshold,
	}
}

// Purge clears the payload store. The vector index retains soft references,
// which are harmless because lookups verify against the store.
func (s *Semantic) Purge() { s.store.Purge() }

// atomicFloat is a tiny lock-free float accumulator built on a uint64 bit-cast.
type atomicFloat struct{ bits atomic.Uint64 }

func (a *atomicFloat) add(delta float64) {
	for {
		old := a.bits.Load()
		newVal := float64frombits(old) + delta
		if a.bits.CompareAndSwap(old, float64bits(newVal)) {
			return
		}
	}
}

func (a *atomicFloat) load() float64 { return float64frombits(a.bits.Load()) }
