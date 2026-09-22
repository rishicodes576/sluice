// Package server wires the configuration into a running HTTP gateway:
// providers, router, cache, middleware and graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/rishicodes576/sluice/internal/admin"
	"github.com/rishicodes576/sluice/internal/auth"
	"github.com/rishicodes576/sluice/internal/budget"
	"github.com/rishicodes576/sluice/internal/cache"
	"github.com/rishicodes576/sluice/internal/config"
	"github.com/rishicodes576/sluice/internal/embed"
	"github.com/rishicodes576/sluice/internal/observability"
	"github.com/rishicodes576/sluice/internal/provider"
	"github.com/rishicodes576/sluice/internal/proxy"
	"github.com/rishicodes576/sluice/internal/ratelimit"
	"github.com/rishicodes576/sluice/internal/resilience"
	"github.com/rishicodes576/sluice/internal/router"
	"github.com/rishicodes576/sluice/internal/vectorstore"
)

// Server is a fully-wired gateway ready to Run.
type Server struct {
	cfg  config.Config
	log  *slog.Logger
	http *http.Server
	reg  *observability.Registry
}

// Version is set at build time via -ldflags and surfaced on /healthz.
var Version = "dev"

// New builds a Server from configuration.
func New(cfg config.Config, log *slog.Logger) (*Server, error) {
	reg := observability.NewRegistry()

	backends, models, err := buildBackends(cfg)
	if err != nil {
		return nil, err
	}

	retry := resilience.RetryPolicy{
		MaxAttempts: cfg.Router.Retry.MaxAttempts,
		BaseDelay:   config.Dur(cfg.Router.Retry.BaseDelay, 50*time.Millisecond),
		MaxDelay:    config.Dur(cfg.Router.Retry.MaxDelay, 2*time.Second),
	}
	rt := router.New(router.Strategy(cfg.Router.Strategy), backends, retry)

	var sem *cache.Semantic
	if cfg.Cache.Enabled {
		emb := embed.NewLocal(cfg.Cache.Dim)
		var idx vectorstore.Store
		if cfg.Cache.Index == "flat" {
			idx = vectorstore.NewFlat(emb.Dim())
		} else {
			idx = vectorstore.NewHNSW(emb.Dim())
		}
		store := cache.NewMemory(cfg.Cache.Shards, config.Dur(cfg.Cache.TTL, time.Hour))
		sem = cache.New(emb, idx, store, cache.Options{Threshold: cfg.Cache.Threshold})
	}

	var limiter *ratelimit.Limiter
	if cfg.RateLimit.Enabled {
		limiter = ratelimit.New(cfg.RateLimit.RPS, cfg.RateLimit.Burst)
	}
	var budgetMgr *budget.Manager
	if cfg.Budget.Enabled {
		budgetMgr = budget.New(cfg.Budget.MaxCostUSD, cfg.Budget.MaxTokens, budget.Window(cfg.Budget.Window))
	}

	metrics := proxy.NewMetrics(reg)
	px := proxy.New(proxy.Deps{
		Router: rt, Cache: sem, Limiter: limiter, RateLimitPerModel: cfg.RateLimit.PerModel,
		Budget: budgetMgr, Metrics: metrics, Logger: log, Models: models,
	})
	adminH := admin.New(sem, rt, Version)
	authN := auth.New(toAuthKeys(cfg.Auth.Keys))

	mux := http.NewServeMux()
	px.Routes(mux)
	adminH.Routes(mux)
	mux.Handle("GET /metrics", reg.Handler())

	handler := chain(mux, recoverMW(log), traceMW(), logMW(log), authN.Middleware)

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      handler,
		ReadTimeout:  config.Dur(cfg.Server.ReadTimeout, 30*time.Second),
		WriteTimeout: config.Dur(cfg.Server.WriteTimeout, 120*time.Second),
	}
	return &Server{cfg: cfg, log: log, http: srv, reg: reg}, nil
}

// Run starts the server and blocks until ctx is cancelled, then shuts down
// gracefully within the configured timeout.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.http.Addr, err)
	}
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("sluice listening", "addr", s.http.Addr, "version", Version)
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("shutting down")
		shCtx, cancel := context.WithTimeout(context.Background(), config.Dur(s.cfg.Server.ShutdownTimeout, 15*time.Second))
		defer cancel()
		return s.http.Shutdown(shCtx)
	}
}

// Handler exposes the fully-wired handler for integration tests.
func (s *Server) Handler() http.Handler { return s.http.Handler }

func toAuthKeys(keys []config.KeyConfig) []auth.Key {
	out := make([]auth.Key, 0, len(keys))
	for _, k := range keys {
		out = append(out, auth.Key{Secret: k.Secret, Owner: k.Owner})
	}
	return out
}

// buildBackends instantiates providers and their routing metadata from config.
func buildBackends(cfg config.Config) ([]*router.Backend, []provider.Model, error) {
	var backends []*router.Backend
	seenModels := map[string]bool{}
	var models []provider.Model
	hc := &http.Client{Timeout: 120 * time.Second}

	for _, pc := range cfg.Providers {
		var p provider.Provider
		switch pc.Type {
		case "mock":
			opts := []provider.MockOption{}
			if d := config.Dur(pc.Latency, 0); d > 0 {
				opts = append(opts, provider.WithLatency(d))
			}
			if pc.FailEvery > 0 {
				opts = append(opts, provider.WithFailEvery(pc.FailEvery))
			}
			p = provider.NewMock(pc.Name, pc.Models, opts...)
		case "openai":
			p = provider.NewOpenAI(pc.Name, pc.BaseURL, pc.APIKey, pc.Models, hc)
		case "anthropic":
			p = provider.NewAnthropic(pc.Name, pc.BaseURL, pc.APIKey, pc.Models, hc)
		default:
			return nil, nil, fmt.Errorf("unknown provider type %q", pc.Type)
		}
		weight := pc.Weight
		if weight <= 0 {
			weight = 1
		}
		backends = append(backends, &router.Backend{Provider: p, Weight: weight, CostPer1K: pc.CostPer1K})
		for _, m := range pc.Models {
			if !seenModels[m] {
				seenModels[m] = true
				models = append(models, provider.Model{ID: m, Object: "model", Created: time.Now().Unix(), OwnedBy: pc.Name})
			}
		}
	}
	return backends, models, nil
}
