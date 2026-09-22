package router

import (
	"context"
	"testing"
	"time"

	"github.com/rishicodes576/sluice/internal/provider"
	"github.com/rishicodes576/sluice/internal/resilience"
)

func mockBackend(name string, models []string, opts ...provider.MockOption) *Backend {
	return &Backend{Provider: provider.NewMock(name, models, opts...), Weight: 1, CostPer1K: map[string]float64{}}
}

func TestRouterChatBasic(t *testing.T) {
	r := New(RoundRobin, []*Backend{mockBackend("a", []string{"m"})}, resilience.DefaultRetry())
	resp, err := r.Chat(context.Background(), &provider.ChatRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Provider != "a" {
		t.Fatalf("provider = %q, want a", resp.Provider)
	}
}

func TestRouterNoBackendForModel(t *testing.T) {
	r := New(RoundRobin, []*Backend{mockBackend("a", []string{"m"})}, resilience.DefaultRetry())
	_, err := r.Chat(context.Background(), &provider.ChatRequest{Model: "other", Messages: []provider.Message{{Role: "user", Content: "x"}}})
	if err != ErrNoBackend {
		t.Fatalf("err = %v, want ErrNoBackend", err)
	}
}

// TestRouterFailover routes around a backend that always fails to a healthy one.
func TestRouterFailover(t *testing.T) {
	bad := mockBackend("bad", []string{"m"}, provider.WithFailEvery(1))
	good := mockBackend("good", []string{"m"})
	// No retries so the failing backend is exhausted immediately, forcing failover.
	r := New(RoundRobin, []*Backend{bad, good}, resilience.RetryPolicy{MaxAttempts: 1, BaseDelay: time.Millisecond})
	resp, err := r.Chat(context.Background(), &provider.ChatRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("expected failover success, got %v", err)
	}
	if resp.Provider != "good" {
		t.Fatalf("provider = %q, want good", resp.Provider)
	}
}

func TestRouterCostStrategyPrefersCheapest(t *testing.T) {
	cheap := mockBackend("cheap", []string{"m"})
	cheap.CostPer1K = map[string]float64{"m": 0.001}
	pricey := mockBackend("pricey", []string{"m"})
	pricey.CostPer1K = map[string]float64{"m": 0.05}
	r := New(CostOptimized, []*Backend{pricey, cheap}, resilience.DefaultRetry())
	resp, _ := r.Chat(context.Background(), &provider.ChatRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "x"}}})
	if resp.Provider != "cheap" {
		t.Fatalf("cost strategy picked %q, want cheap", resp.Provider)
	}
	if got := r.CostFor("m"); got != 0.001 {
		t.Fatalf("CostFor = %v, want 0.001", got)
	}
}

func TestRouterEmbedFailover(t *testing.T) {
	r := New(RoundRobin, []*Backend{mockBackend("a", []string{"emb"})}, resilience.DefaultRetry())
	resp, err := r.Embed(context.Background(), &provider.EmbedRequest{Model: "emb", Input: []string{"hi", "there"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("got %d embeddings, want 2", len(resp.Data))
	}
}
