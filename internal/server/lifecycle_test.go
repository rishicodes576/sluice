package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/rishicodes576/sluice/internal/config"
)

// TestServerLifecycle builds a server wired with all three provider types,
// starts it on an ephemeral port, hits /healthz, then cancels the context and
// asserts Run shuts down gracefully.
func TestServerLifecycle(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	cfg := config.Default()
	cfg.Server.Addr = addr
	cfg.Providers = append(cfg.Providers,
		config.ProviderConfig{Name: "openai", Type: "openai", BaseURL: "http://127.0.0.1:1", Models: []string{"gpt-4o"}},
		config.ProviderConfig{Name: "anthropic", Type: "anthropic", Models: []string{"claude"}},
	)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(cfg, log)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	// Wait for the listener to come up, then probe health.
	var resp *http.Response
	for i := 0; i < 50; i++ {
		resp, err = http.Get("http://" + addr + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("healthz never became reachable: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on graceful shutdown", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shut down in time")
	}
}
