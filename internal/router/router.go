// Package router selects an upstream provider for each request and tracks
// per-provider health so unhealthy backends are avoided and failover is
// automatic.
package router

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rishicodes576/sluice/internal/provider"
	"github.com/rishicodes576/sluice/internal/resilience"
)

// Strategy chooses which provider to try first among healthy candidates.
type Strategy string

const (
	// RoundRobin distributes load evenly.
	RoundRobin Strategy = "round-robin"
	// Weighted biases toward providers with higher configured weight.
	Weighted Strategy = "weighted"
	// LatencyEWMA prefers the provider with the lowest smoothed latency.
	LatencyEWMA Strategy = "latency"
	// CostOptimized prefers the cheapest provider for the model.
	CostOptimized Strategy = "cost"
)

// Backend wraps a Provider with routing metadata and live health state.
type Backend struct {
	Provider provider.Provider
	Weight   int
	// CostPer1K maps model -> USD per 1K total tokens, used by CostOptimized
	// and for savings accounting.
	CostPer1K map[string]float64

	breaker *resilience.Breaker
	ewma    atomicFloat // smoothed latency in milliseconds
}

// EWMA returns the current smoothed latency in milliseconds.
func (b *Backend) EWMA() float64 { return b.ewma.load() }

// Healthy reports whether the backend's breaker currently allows traffic.
func (b *Backend) Healthy() bool { return b.breaker.State() != resilience.Open }

const ewmaAlpha = 0.2

func (b *Backend) observe(d time.Duration) {
	ms := float64(d.Milliseconds())
	for {
		old := b.ewma.load()
		next := ms
		if old > 0 {
			next = ewmaAlpha*ms + (1-ewmaAlpha)*old
		}
		if b.ewma.cas(old, next) {
			return
		}
	}
}

// Router routes requests across backends for a given model.
type Router struct {
	strategy Strategy
	retry    resilience.RetryPolicy

	mu       sync.RWMutex
	backends []*Backend
	rrCursor atomic.Uint64
}

// New creates a Router.
func New(strategy Strategy, backends []*Backend, retry resilience.RetryPolicy) *Router {
	if strategy == "" {
		strategy = RoundRobin
	}
	for _, b := range backends {
		if b.breaker == nil {
			b.breaker = resilience.NewBreaker(5, 10*time.Second)
		}
	}
	return &Router{strategy: strategy, backends: backends, retry: retry}
}

// ErrNoBackend indicates no healthy backend serves the requested model.
var ErrNoBackend = errors.New("router: no healthy backend for model")

// candidates returns healthy backends serving model, ordered by the strategy.
func (r *Router) candidates(model string) []*Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var serving []*Backend
	for _, b := range r.backends {
		if serves(b.Provider, model) {
			serving = append(serving, b)
		}
	}
	// Prefer healthy backends but keep unhealthy ones as last-resort failover.
	sort.SliceStable(serving, func(i, j int) bool {
		hi, hj := serving[i].Healthy(), serving[j].Healthy()
		if hi != hj {
			return hi
		}
		return r.prefer(serving[i], serving[j], model)
	})
	return serving
}

// prefer implements the strategy-specific ordering among equally-healthy peers.
func (r *Router) prefer(a, b *Backend, model string) bool {
	switch r.strategy {
	case LatencyEWMA:
		ea, eb := a.EWMA(), b.EWMA()
		if ea == 0 {
			return true // untried backends get an early chance
		}
		if eb == 0 {
			return false
		}
		return ea < eb
	case CostOptimized:
		return a.CostPer1K[model] < b.CostPer1K[model]
	case Weighted:
		return a.Weight > b.Weight
	default: // RoundRobin: rotate the starting point
		start := int(r.rrCursor.Add(1))
		return (indexOf(r.backends, a)+start)%len(r.backends) <
			(indexOf(r.backends, b)+start)%len(r.backends)
	}
}

// Chat routes a non-streaming completion with retries and failover.
func (r *Router) Chat(ctx context.Context, req *provider.ChatRequest) (*provider.ChatResponse, error) {
	cands := r.candidates(req.Model)
	if len(cands) == 0 {
		return nil, ErrNoBackend
	}
	var lastErr error
	for _, b := range cands {
		var resp *provider.ChatResponse
		err := r.retry.Retry(ctx, func(ctx context.Context) error {
			return b.breaker.Do(func() error {
				start := time.Now()
				out, cerr := b.Provider.Chat(ctx, req)
				b.observe(time.Since(start))
				if cerr != nil {
					return cerr
				}
				resp = out
				return nil
			})
		})
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// Stream routes a streaming completion, failing over to the next backend only
// if establishing the stream fails (mid-stream errors surface to the caller).
func (r *Router) Stream(ctx context.Context, req *provider.ChatRequest) (<-chan provider.StreamEvent, string, error) {
	cands := r.candidates(req.Model)
	if len(cands) == 0 {
		return nil, "", ErrNoBackend
	}
	var lastErr error
	for _, b := range cands {
		if !b.breaker.Allow() {
			lastErr = resilience.ErrOpen
			continue
		}
		start := time.Now()
		ch, err := b.Provider.Stream(ctx, req)
		if err != nil {
			b.observe(time.Since(start))
			b.breaker.Failure()
			lastErr = err
			continue
		}
		b.breaker.Success()
		return ch, b.Provider.Name(), nil
	}
	return nil, "", lastErr
}

// Embed routes an embeddings request with failover.
func (r *Router) Embed(ctx context.Context, req *provider.EmbedRequest) (*provider.EmbedResponse, error) {
	cands := r.candidates(req.Model)
	if len(cands) == 0 {
		return nil, ErrNoBackend
	}
	var lastErr error
	for _, b := range cands {
		out, err := b.Provider.Embed(ctx, req)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// CostFor returns the configured USD-per-1K-tokens for a model, using the
// cheapest backend that serves it, or 0 if unknown.
func (r *Router) CostFor(model string) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	best := 0.0
	found := false
	for _, b := range r.backends {
		if c, ok := b.CostPer1K[model]; ok {
			if !found || c < best {
				best, found = c, true
			}
		}
	}
	return best
}

// Backends returns the routed backends (read-only use).
func (r *Router) Backends() []*Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Backend, len(r.backends))
	copy(out, r.backends)
	return out
}

func serves(p provider.Provider, model string) bool {
	for _, m := range p.Models() {
		if m == model {
			return true
		}
	}
	return false
}

func indexOf(bs []*Backend, target *Backend) int {
	for i, b := range bs {
		if b == target {
			return i
		}
	}
	return 0
}

// atomicFloat is a lock-free float accumulator (IEEE-754 bit-cast).
type atomicFloat struct{ bits atomic.Uint64 }

func (a *atomicFloat) load() float64 { return float64FromBits(a.bits.Load()) }
func (a *atomicFloat) cas(old, new float64) bool {
	return a.bits.CompareAndSwap(bitsOf(old), bitsOf(new))
}
