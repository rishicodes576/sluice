package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rishicodes576/sluice/internal/config"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Default()
	cfg.Providers[0].Latency = "" // no artificial latency in tests
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler()
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEndToEndChatAndCache(t *testing.T) {
	h := testServer(t)
	body := `{"model":"mock-1","messages":[{"role":"user","content":"what is the capital of France"}]}`

	// First call: cache miss, served by the mock provider.
	r1 := post(t, h, "/v1/chat/completions", body)
	if r1.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", r1.Code, r1.Body.String())
	}
	if got := r1.Header().Get("X-Sluice-Cache"); got != "miss" {
		t.Fatalf("first call cache header = %q, want miss", got)
	}
	var resp map[string]any
	if err := json.Unmarshal(r1.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp["object"] != "chat.completion" {
		t.Fatalf("unexpected object %v", resp["object"])
	}

	// Second identical call: semantic cache hit.
	r2 := post(t, h, "/v1/chat/completions", body)
	if got := r2.Header().Get("X-Sluice-Cache"); got != "hit" {
		t.Fatalf("second call cache header = %q, want hit", got)
	}
	if !bytes.Equal(r1.Body.Bytes(), r2.Body.Bytes()) {
		t.Fatal("cached response body differs from original")
	}
}

func TestEndToEndStreaming(t *testing.T) {
	h := testServer(t)
	body := `{"model":"mock-1","stream":true,"messages":[{"role":"user","content":"stream some words please"}]}`
	rec := post(t, h, "/v1/chat/completions", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "data: ") || !strings.Contains(out, "[DONE]") {
		t.Fatalf("stream output malformed:\n%s", out)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestEndToEndValidation(t *testing.T) {
	h := testServer(t)
	rec := post(t, h, "/v1/chat/completions", `{"messages":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHealthAndMetrics(t *testing.T) {
	h := testServer(t)
	// Generate one request so metrics are populated.
	post(t, h, "/v1/chat/completions", `{"model":"mock-1","messages":[{"role":"user","content":"hi"}]}`)

	hz := httptest.NewRecorder()
	h.ServeHTTP(hz, httptest.NewRequest("GET", "/healthz", nil))
	if hz.Code != http.StatusOK {
		t.Fatalf("healthz = %d", hz.Code)
	}

	mrec := httptest.NewRecorder()
	h.ServeHTTP(mrec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(mrec.Body.String(), "sluice_requests_total") {
		t.Fatalf("metrics missing request counter:\n%s", mrec.Body.String())
	}

	srec := httptest.NewRecorder()
	h.ServeHTTP(srec, httptest.NewRequest("GET", "/admin/stats", nil))
	if !strings.Contains(srec.Body.String(), "cache") {
		t.Fatalf("stats missing cache section:\n%s", srec.Body.String())
	}
}

func TestModelsEndpoint(t *testing.T) {
	h := testServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "mock-1") {
		t.Fatalf("models endpoint: code=%d body=%s", rec.Code, rec.Body.String())
	}
}
