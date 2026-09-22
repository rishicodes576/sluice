package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rishicodes576/sluice/internal/cache"
	"github.com/rishicodes576/sluice/internal/embed"
	"github.com/rishicodes576/sluice/internal/provider"
	"github.com/rishicodes576/sluice/internal/resilience"
	"github.com/rishicodes576/sluice/internal/router"
	"github.com/rishicodes576/sluice/internal/vectorstore"
)

func newAdmin(t *testing.T, failEvery int) (*Admin, *router.Router) {
	t.Helper()
	opts := []provider.MockOption{}
	if failEvery > 0 {
		opts = append(opts, provider.WithFailEvery(failEvery))
	}
	b := &router.Backend{Provider: provider.NewMock("mock", []string{"m"}, opts...), Weight: 1, CostPer1K: map[string]float64{}}
	rt := router.New(router.RoundRobin, []*router.Backend{b}, resilience.RetryPolicy{MaxAttempts: 1})
	emb := embed.NewLocal(64)
	sem := cache.New(emb, vectorstore.NewHNSW(emb.Dim(), vectorstore.WithSeed(1)), cache.NewMemory(4, time.Hour), cache.Options{Threshold: 0.9})
	return New(sem, rt, "test"), rt
}

func serve(a *Admin, method, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	a.Routes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	a, _ := newAdmin(t, 0)
	rec := serve(a, "GET", "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["version"] != "test" {
		t.Fatalf("version = %q", body["version"])
	}
}

func TestReadyzHealthy(t *testing.T) {
	a, _ := newAdmin(t, 0)
	if rec := serve(a, "GET", "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
}

func TestReadyzUnhealthy(t *testing.T) {
	a, rt := newAdmin(t, 1) // every call fails
	// Trip the breaker by driving failures through the router.
	for i := 0; i < 6; i++ {
		_, _ = rt.Chat(context.Background(), &provider.ChatRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "x"}}})
	}
	if rec := serve(a, "GET", "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}

func TestStatsAndBackends(t *testing.T) {
	a, _ := newAdmin(t, 0)
	rec := serve(a, "GET", "/admin/stats")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats code = %d", rec.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if _, ok := out["cache"]; !ok {
		t.Fatal("stats missing cache")
	}
	if _, ok := out["backends"]; !ok {
		t.Fatal("stats missing backends")
	}
	if b := serve(a, "GET", "/admin/backends"); b.Code != http.StatusOK {
		t.Fatalf("backends code = %d", b.Code)
	}
}

func TestPurge(t *testing.T) {
	a, _ := newAdmin(t, 0)
	rec := serve(a, "POST", "/admin/cache/purge")
	if rec.Code != http.StatusOK {
		t.Fatalf("purge code = %d", rec.Code)
	}
}

func TestPurgeCacheDisabled(t *testing.T) {
	b := &router.Backend{Provider: provider.NewMock("mock", []string{"m"}), Weight: 1}
	rt := router.New(router.RoundRobin, []*router.Backend{b}, resilience.DefaultRetry())
	a := New(nil, rt, "test")
	if rec := serve(a, "POST", "/admin/cache/purge"); rec.Code != http.StatusOK {
		t.Fatalf("purge (no cache) code = %d", rec.Code)
	}
}
