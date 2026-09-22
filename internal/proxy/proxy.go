// Package proxy implements the OpenAI-compatible HTTP handlers that front the
// router and semantic cache.
package proxy

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rishicodes576/sluice/internal/auth"
	"github.com/rishicodes576/sluice/internal/budget"
	"github.com/rishicodes576/sluice/internal/cache"
	"github.com/rishicodes576/sluice/internal/provider"
	"github.com/rishicodes576/sluice/internal/ratelimit"
	"github.com/rishicodes576/sluice/internal/router"
)

// Proxy holds the dependencies for the request-handling layer.
type Proxy struct {
	router   *router.Router
	cache    *cache.Semantic // nil when caching is disabled
	limiter  *ratelimit.Limiter
	perModel bool
	budget   *budget.Manager
	metrics  *Metrics
	log      *slog.Logger
	models   []provider.Model
}

// Deps are the injected dependencies for a Proxy.
type Deps struct {
	Router            *router.Router
	Cache             *cache.Semantic
	Limiter           *ratelimit.Limiter
	RateLimitPerModel bool
	Budget            *budget.Manager
	Metrics           *Metrics
	Logger            *slog.Logger
	Models            []provider.Model
}

// New constructs a Proxy.
func New(d Deps) *Proxy {
	return &Proxy{
		router: d.Router, cache: d.Cache, limiter: d.Limiter, perModel: d.RateLimitPerModel,
		budget: d.Budget, metrics: d.Metrics, log: d.Logger, models: d.Models,
	}
}

// Routes registers the proxy's HTTP handlers on the given mux.
func (p *Proxy) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", p.chatCompletions)
	mux.HandleFunc("POST /v1/embeddings", p.embeddings)
	mux.HandleFunc("GET /v1/models", p.listModels)
}

func writeError(w http.ResponseWriter, status int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": msg, "type": typ},
	})
}

func mapErr(w http.ResponseWriter, err error) {
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		writeError(w, apiErr.Status, apiErr.Type, apiErr.Message)
		return
	}
	if errors.Is(err, router.ErrNoBackend) {
		writeError(w, http.StatusServiceUnavailable, "no_backend", err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, "upstream_error", err.Error())
}

// gate applies rate limiting and budget checks; returns false if the request
// was rejected (and a response already written).
func (p *Proxy) gate(w http.ResponseWriter, owner, model string) bool {
	if p.limiter != nil {
		key := owner
		if p.perModel {
			key = owner + ":" + model
		}
		if !p.limiter.Allow(key) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded")
			return false
		}
	}
	if p.budget != nil && !p.budget.Allow(owner) {
		writeError(w, http.StatusPaymentRequired, "budget_exceeded", "budget exceeded for this key")
		return false
	}
	return true
}

func promptKey(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Role)
		b.WriteByte(':')
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func (p *Proxy) costFor(model string, tokens int) float64 {
	return p.router.CostFor(model) * float64(tokens) / 1000.0
}

func (p *Proxy) chatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := auth.Owner(ctx)
	start := time.Now()

	var req provider.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "model and messages are required")
		return
	}
	if !p.gate(w, owner, req.Model) {
		return
	}

	if req.Stream {
		p.streamChat(w, r, &req, owner, start)
		return
	}

	prompt := promptKey(req.Messages)
	if p.cache != nil {
		if entry, ok, err := p.cache.Lookup(ctx, req.Model, prompt); err == nil && ok {
			p.metrics.CacheReq.Inc(req.Model, "hit")
			w.Header().Set("X-Sluice-Cache", "hit")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(entry.Response)
			p.observe("chat", req.Model, "cache", "200", start)
			return
		}
		p.metrics.CacheReq.Inc(req.Model, "miss")
	}

	resp, err := p.router.Chat(ctx, &req)
	if err != nil {
		p.metrics.Requests.Inc("chat", req.Model, "none", "error")
		mapErr(w, err)
		return
	}

	body, _ := json.Marshal(resp)
	cost := p.costFor(req.Model, resp.Usage.TotalTokens)
	if p.cache != nil {
		_ = p.cache.Store(ctx, req.Model, prompt, body, resp.Usage.TotalTokens, cost)
	}
	p.account(owner, req.Model, resp.Usage, cost)

	w.Header().Set("X-Sluice-Cache", "miss")
	w.Header().Set("X-Sluice-Provider", resp.Provider)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
	p.observe("chat", req.Model, resp.Provider, "200", start)
}

func (p *Proxy) streamChat(w http.ResponseWriter, r *http.Request, req *provider.ChatRequest, owner string, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "server_error", "streaming unsupported")
		return
	}
	ch, prov, err := p.router.Stream(r.Context(), req)
	if err != nil {
		mapErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Sluice-Provider", prov)
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	for ev := range ch {
		if ev.Err != nil {
			break
		}
		if ev.Done {
			break
		}
		if ev.Chunk != nil {
			_, _ = w.Write([]byte("data: "))
			_ = enc.Encode(ev.Chunk) // Encode appends a newline
			_, _ = w.Write([]byte("\n"))
			flusher.Flush()
		}
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
	p.observe("chat_stream", req.Model, prov, "200", start)
}

func (p *Proxy) embeddings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := auth.Owner(ctx)
	start := time.Now()

	var req provider.EmbedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	if req.Model == "" || len(req.Input) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "model and input are required")
		return
	}
	if !p.gate(w, owner, req.Model) {
		return
	}
	resp, err := p.router.Embed(ctx, &req)
	if err != nil {
		mapErr(w, err)
		return
	}
	cost := p.costFor(req.Model, resp.Usage.TotalTokens)
	p.account(owner, req.Model, resp.Usage, cost)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
	p.observe("embeddings", req.Model, "", "200", start)
}

func (p *Proxy) listModels(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": p.models})
}

// account records tokens, cost and budget usage and refreshes upstream gauges.
func (p *Proxy) account(owner, model string, u provider.Usage, cost float64) {
	p.metrics.Tokens.Add(float64(u.PromptTokens), model, "prompt")
	p.metrics.Tokens.Add(float64(u.CompletionTokens), model, "completion")
	p.metrics.CostUSD.Add(cost, model)
	if p.budget != nil {
		p.budget.Record(owner, cost, int64(u.TotalTokens))
	}
	for _, b := range p.router.Backends() {
		p.metrics.UpstreamL.Set(b.EWMA(), b.Provider.Name())
	}
}

func (p *Proxy) observe(endpoint, model, provName, status string, start time.Time) {
	p.metrics.Requests.Inc(endpoint, model, provName, status)
	p.metrics.Latency.Observe(time.Since(start).Seconds(), endpoint, model)
}
