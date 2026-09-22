// Package embed turns text into unit-normalised vectors for the semantic cache.
// A dependency-free local vectorizer is provided so the cache works offline;
// production deployments can swap in a provider-backed embedder.
package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"
)

// Embedder maps a batch of texts to vectors of a fixed dimension.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
	Dim() int
}

// Local is a deterministic feature-hashing embedder over unigrams and bigrams.
// It requires no model download or network call, yet places semantically
// similar prompts near each other, which is sufficient to demonstrate and test
// semantic caching. Swap for a provider-backed embedder in production.
type Local struct{ dim int }

// NewLocal creates a local embedder of the given dimension (256 if <= 0).
func NewLocal(dim int) *Local {
	if dim <= 0 {
		dim = 256
	}
	return &Local{dim: dim}
}

// Dim implements Embedder.
func (l *Local) Dim() int { return l.dim }

// Embed implements Embedder.
func (l *Local) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		out[i] = l.vectorize(t)
	}
	return out, nil
}

func (l *Local) vectorize(s string) []float64 {
	v := make([]float64, l.dim)
	toks := strings.Fields(strings.ToLower(s))
	add := func(feat string) {
		h := sha256.Sum256([]byte(feat))
		idx := binary.BigEndian.Uint32(h[:4]) % uint32(l.dim)
		sign := 1.0
		if h[4]&1 == 1 {
			sign = -1.0
		}
		v[idx] += sign
	}
	for i, t := range toks {
		add(t)
		if i > 0 {
			add(toks[i-1] + " " + t)
		}
	}
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	for i := range v {
		v[i] /= norm
	}
	return v
}
