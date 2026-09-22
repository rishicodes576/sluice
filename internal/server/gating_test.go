package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rishicodes576/sluice/internal/config"
)

// TestRateLimitAndAuthEnforced wires a server with auth + a tight rate limit
// and asserts both gates fire.
func TestRateLimitAndAuthEnforced(t *testing.T) {
	cfg := config.Default()
	cfg.Providers[0].Latency = ""
	cfg.Auth.Keys = []config.KeyConfig{{Secret: "sk-test", Owner: "team"}}
	cfg.RateLimit = config.RateLimitConfig{Enabled: true, RPS: 0.0001, Burst: 1}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	body := `{"model":"mock-1","messages":[{"role":"user","content":"hi"}]}`

	// Missing key -> 401.
	unauth := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	unauth.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unauth)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key: code=%d, want 401", rec.Code)
	}

	call := func() int {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sk-test")
		r := httptest.NewRecorder()
		h.ServeHTTP(r, req)
		return r.Code
	}
	if code := call(); code != http.StatusOK {
		t.Fatalf("first authed call: code=%d, want 200", code)
	}
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("second call within burst: code=%d, want 429", code)
	}
}
