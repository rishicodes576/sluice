// Package vectorstore provides approximate and exact nearest-neighbour search
// over unit-normalised embedding vectors, backing Sluice's semantic cache.
package vectorstore

import (
	"errors"
	"math"
	"sync"
)

// Match is a search result: the stored item's id and its cosine similarity to
// the query (1.0 == identical direction).
type Match struct {
	ID         string
	Similarity float64
}

// Store indexes vectors by id and answers nearest-neighbour queries.
// Implementations must be safe for concurrent use.
type Store interface {
	Add(id string, vec []float64) error
	Search(vec []float64, k int) []Match
	Delete(id string)
	Len() int
}

// ErrDimMismatch is returned when a vector's length does not match the index.
var ErrDimMismatch = errors.New("vectorstore: vector dimension mismatch")

func cosine(a, b []float64) float64 {
	// Vectors are stored normalised, so cosine reduces to a dot product; we
	// still guard against non-normalised input for correctness.
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Flat is an exact brute-force store. It is simple, correct, and used as the
// reference implementation and for small indexes.
type Flat struct {
	mu   sync.RWMutex
	dim  int
	ids  []string
	vecs [][]float64
	idx  map[string]int
}

// NewFlat creates an exact store for vectors of the given dimension.
func NewFlat(dim int) *Flat {
	return &Flat{dim: dim, idx: make(map[string]int)}
}

// Add implements Store.
func (f *Flat) Add(id string, vec []float64) error {
	if len(vec) != f.dim {
		return ErrDimMismatch
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if i, ok := f.idx[id]; ok {
		f.vecs[i] = vec
		return nil
	}
	f.idx[id] = len(f.ids)
	f.ids = append(f.ids, id)
	f.vecs = append(f.vecs, vec)
	return nil
}

// Search implements Store, returning up to k best matches sorted descending.
func (f *Flat) Search(vec []float64, k int) []Match {
	f.mu.RLock()
	defer f.mu.RUnlock()
	matches := make([]Match, 0, len(f.ids))
	for i, id := range f.ids {
		if f.ids[i] == "" {
			continue
		}
		matches = append(matches, Match{ID: id, Similarity: cosine(vec, f.vecs[i])})
	}
	return topK(matches, k)
}

// Delete implements Store.
func (f *Flat) Delete(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i, ok := f.idx[id]; ok {
		f.ids[i] = "" // tombstone; skipped during search
		delete(f.idx, id)
	}
}

// Len implements Store.
func (f *Flat) Len() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.idx)
}

// topK returns the k highest-similarity matches via partial selection sort,
// which is cheap for the small k typical of caching.
func topK(matches []Match, k int) []Match {
	if k <= 0 {
		k = 1
	}
	if k > len(matches) {
		k = len(matches)
	}
	for i := 0; i < k; i++ {
		best := i
		for j := i + 1; j < len(matches); j++ {
			if matches[j].Similarity > matches[best].Similarity {
				best = j
			}
		}
		matches[i], matches[best] = matches[best], matches[i]
	}
	return matches[:k]
}
