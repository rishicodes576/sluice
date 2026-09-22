package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rishicodes576/sluice/internal/budget"
	"github.com/rishicodes576/sluice/internal/cache"
	"github.com/rishicodes576/sluice/internal/embed"
	"github.com/rishicodes576/sluice/internal/observability"
	"github.com/rishicodes576/sluice/internal/provider"
	"github.com/rishicodes576/sluice/internal/ratelimit"
	"github.com/rishicodes576/sluice/internal/resilience"
	"github.com/rishicodes576/sluice/internal/router"
	"github.com/rishicodes576/sluice/internal/vectorstore"
)

func newTestProxy(t *testing.T, d Deps) http.Handler {
	t.Helper()
	if d.Metrics == nil {
		d.Metrics = NewMetrics(observability.NewRegistry())
	}
	if d.Logger == nil {
		d.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if d.Router == nil {
		b := &router.Backend{Provider: provider.NewMock("mock", []string{"m", "emb"}), Weight: 1, CostPer1K: map[string]float64{"m": 0.01}}
		d.Router = router.New(router.RoundRobin, []*router.Backend{b}, resilience.DefaultRetry())
	}
	d.Models = []provider.Model{{ID: "m", Object: "model"}}
	p := New(d)
	mux := http.NewServeMux()
	p.Routes(mux)
	return mux
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestProxyEmbeddings(t *testing.T) {
	h := newTestProxy(t, Deps{})
	rec := do(h, "POST", "/v1/embeddings", `{"model":"emb","input":["a","b"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp provider.EmbedResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Data) != 2 {
		t.Fatalf("data = %d, want 2", len(resp.Data))
	}
}

func TestProxyEmbeddingsValidation(t *testing.T) {
	h := newTestProxy(t, Deps{})
	if rec := do(h, "POST", "/v1/embeddings", `{"model":"emb"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestProxyModels(t *testing.T) {
	h := newTestProxy(t, Deps{})
	rec := do(h, "GET", "/v1/models", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"m"`) {
		t.Fatalf("models: %d %s", rec.Code, rec.Body.String())
	}
}

func TestProxyNoBackendReturns503(t *testing.T) {
	h := newTestProxy(t, Deps{})
	rec := do(h, "POST", "/v1/chat/completions", `{"model":"unknown","messages":[{"role":"user","content":"x"}]}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}

func TestProxyBudgetGate(t *testing.T) {
	// Budget already exhausted -> 402 before hitting the router.
	b := budget.New(0.001, 0, budget.Daily)
	b.Record("anonymous", 1.0, 1000)
	h := newTestProxy(t, Deps{Budget: b})
	rec := do(h, "POST", "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"x"}]}`)
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("code = %d, want 402", rec.Code)
	}
}

func TestProxyRateLimitGate(t *testing.T) {
	h := newTestProxy(t, Deps{Limiter: ratelimit.New(0.0001, 1)})
	body := `{"model":"m","messages":[{"role":"user","content":"x"}]}`
	if rec := do(h, "POST", "/v1/chat/completions", body); rec.Code != http.StatusOK {
		t.Fatalf("first call = %d, want 200", rec.Code)
	}
	if rec := do(h, "POST", "/v1/chat/completions", body); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second call = %d, want 429", rec.Code)
	}
}

func TestProxyCacheStoreAndHit(t *testing.T) {
	emb := embed.NewLocal(128)
	sem := cache.New(emb, vectorstore.NewHNSW(emb.Dim(), vectorstore.WithSeed(1)), cache.NewMemory(4, time.Hour), cache.Options{Threshold: 0.9})
	h := newTestProxy(t, Deps{Cache: sem})
	body := `{"model":"m","messages":[{"role":"user","content":"cache me please friend"}]}`

	r1 := do(h, "POST", "/v1/chat/completions", body)
	if r1.Header().Get("X-Sluice-Cache") != "miss" {
		t.Fatalf("first cache header = %q, want miss", r1.Header().Get("X-Sluice-Cache"))
	}
	r2 := do(h, "POST", "/v1/chat/completions", body)
	if r2.Header().Get("X-Sluice-Cache") != "hit" {
		t.Fatalf("second cache header = %q, want hit", r2.Header().Get("X-Sluice-Cache"))
	}
	if sem.Stats().Hits != 1 {
		t.Fatalf("cache hits = %d, want 1", sem.Stats().Hits)
	}
}

func TestProxyInvalidJSON(t *testing.T) {
	h := newTestProxy(t, Deps{})
	if rec := do(h, "POST", "/v1/chat/completions", `{not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}
