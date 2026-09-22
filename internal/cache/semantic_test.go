package cache

import (
	"context"
	"testing"
	"time"

	"github.com/rishicodes576/sluice/internal/embed"
	"github.com/rishicodes576/sluice/internal/vectorstore"
)

func newTestCache(threshold float64) *Semantic {
	emb := embed.NewLocal(256)
	idx := vectorstore.NewHNSW(emb.Dim(), vectorstore.WithSeed(1))
	store := NewMemory(4, time.Hour)
	return New(emb, idx, store, Options{Threshold: threshold})
}

func TestSemanticHitOnIdenticalPrompt(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(0.9)
	if err := c.Store(ctx, "gpt", "what is the capital of france", []byte(`{"ok":true}`), 10, 0.01); err != nil {
		t.Fatal(err)
	}
	entry, ok, err := c.Lookup(ctx, "gpt", "what is the capital of france")
	if err != nil || !ok {
		t.Fatalf("expected hit, ok=%v err=%v", ok, err)
	}
	if string(entry.Response) != `{"ok":true}` {
		t.Fatalf("unexpected payload %q", entry.Response)
	}
}

func TestSemanticMissAcrossModels(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(0.9)
	_ = c.Store(ctx, "model-a", "hello world", []byte(`x`), 2, 0)
	if _, ok, _ := c.Lookup(ctx, "model-b", "hello world"); ok {
		t.Fatal("cache must not serve a response across different models")
	}
}

func TestSemanticMissBelowThreshold(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(0.999)
	_ = c.Store(ctx, "gpt", "the quick brown fox jumps over the lazy dog", []byte(`x`), 5, 0)
	if _, ok, _ := c.Lookup(ctx, "gpt", "completely different unrelated sentence about databases"); ok {
		t.Fatal("dissimilar prompt should miss at high threshold")
	}
}

func TestStatsTrackHits(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(0.9)
	_ = c.Store(ctx, "gpt", "alpha beta gamma", []byte(`x`), 7, 0.02)
	_, _, _ = c.Lookup(ctx, "gpt", "alpha beta gamma")
	_, _, _ = c.Lookup(ctx, "gpt", "zzz nothing like it at all friend")
	s := c.Stats()
	if s.Hits != 1 || s.Lookups != 2 {
		t.Fatalf("stats = %+v", s)
	}
	if s.CostSaved <= 0 {
		t.Fatalf("expected cost saved > 0, got %v", s.CostSaved)
	}
}
