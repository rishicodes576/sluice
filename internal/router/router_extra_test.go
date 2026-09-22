package router

import (
	"context"
	"testing"
	"time"

	"github.com/rishicodes576/sluice/internal/provider"
	"github.com/rishicodes576/sluice/internal/resilience"
)

func TestWeightedPrefersHigherWeight(t *testing.T) {
	low := mockBackend("low", []string{"m"})
	low.Weight = 1
	high := mockBackend("high", []string{"m"})
	high.Weight = 100
	r := New(Weighted, []*Backend{low, high}, resilience.DefaultRetry())
	resp, _ := r.Chat(context.Background(), &provider.ChatRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "x"}}})
	if resp.Provider != "high" {
		t.Fatalf("weighted picked %q, want high", resp.Provider)
	}
}

func TestLatencyStrategyPrefersFaster(t *testing.T) {
	slow := mockBackend("slow", []string{"m"}, provider.WithLatency(30*time.Millisecond))
	fast := mockBackend("fast", []string{"m"}, provider.WithLatency(1*time.Millisecond))
	r := New(LatencyEWMA, []*Backend{slow, fast}, resilience.DefaultRetry())
	// Warm up EWMA for both backends.
	for i := 0; i < 5; i++ {
		_, _ = slow.Provider.Chat(context.Background(), &provider.ChatRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "x"}}})
	}
	// Drive traffic through the router so it records latencies, then confirm the
	// faster backend wins subsequent routing.
	for i := 0; i < 8; i++ {
		_, _ = r.Chat(context.Background(), &provider.ChatRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "x"}}})
	}
	if fast.EWMA() == 0 {
		t.Fatal("expected fast backend to have recorded latency")
	}
	resp, _ := r.Chat(context.Background(), &provider.ChatRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "x"}}})
	if resp.Provider != "fast" {
		t.Fatalf("latency strategy picked %q, want fast", resp.Provider)
	}
}

func TestRouterStream(t *testing.T) {
	r := New(RoundRobin, []*Backend{mockBackend("a", []string{"m"})}, resilience.DefaultRetry())
	ch, prov, err := r.Stream(context.Background(), &provider.ChatRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "one two"}}})
	if err != nil || prov != "a" {
		t.Fatalf("stream err=%v prov=%q", err, prov)
	}
	count := 0
	for ev := range ch {
		if ev.Chunk != nil {
			count++
		}
	}
	if count == 0 {
		t.Fatal("no chunks streamed")
	}
}

func TestRouterStreamNoBackend(t *testing.T) {
	r := New(RoundRobin, []*Backend{mockBackend("a", []string{"m"})}, resilience.DefaultRetry())
	if _, _, err := r.Stream(context.Background(), &provider.ChatRequest{Model: "nope", Messages: []provider.Message{{Role: "user", Content: "x"}}}); err != ErrNoBackend {
		t.Fatalf("err = %v, want ErrNoBackend", err)
	}
}

func TestCostForUnknownModel(t *testing.T) {
	r := New(RoundRobin, []*Backend{mockBackend("a", []string{"m"})}, resilience.DefaultRetry())
	if c := r.CostFor("does-not-exist"); c != 0 {
		t.Fatalf("CostFor unknown = %v, want 0", c)
	}
}

func TestBackendsSnapshot(t *testing.T) {
	r := New(RoundRobin, []*Backend{mockBackend("a", []string{"m"}), mockBackend("b", []string{"m"})}, resilience.DefaultRetry())
	if len(r.Backends()) != 2 {
		t.Fatalf("backends = %d, want 2", len(r.Backends()))
	}
}
