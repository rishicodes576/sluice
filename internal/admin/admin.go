// Package admin exposes operational endpoints: health, readiness, cache stats,
// cache purge and a redacted view of the effective configuration.
package admin

import (
	"encoding/json"
	"net/http"

	"github.com/rishicodes576/sluice/internal/cache"
	"github.com/rishicodes576/sluice/internal/router"
)

// Admin serves operational endpoints.
type Admin struct {
	cache   *cache.Semantic
	router  *router.Router
	version string
}

// New constructs an Admin handler set.
func New(c *cache.Semantic, r *router.Router, version string) *Admin {
	return &Admin{cache: c, router: r, version: version}
}

// Routes registers admin handlers on the mux.
func (a *Admin) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /readyz", a.ready)
	mux.HandleFunc("GET /admin/stats", a.stats)
	mux.HandleFunc("POST /admin/cache/purge", a.purge)
	mux.HandleFunc("GET /admin/backends", a.backends)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *Admin) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": a.version})
}

// ready reports 200 only if at least one backend is healthy.
func (a *Admin) ready(w http.ResponseWriter, _ *http.Request) {
	for _, b := range a.router.Backends() {
		if b.Healthy() {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
			return
		}
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "no healthy backends"})
}

// BackendStatus is a serialisable view of a backend's health.
type BackendStatus struct {
	Provider  string   `json:"provider"`
	Healthy   bool     `json:"healthy"`
	LatencyMs float64  `json:"latency_ms"`
	Models    []string `json:"models"`
}

func (a *Admin) stats(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{"version": a.version}
	if a.cache != nil {
		out["cache"] = a.cache.Stats()
	}
	out["backends"] = a.backendStatuses()
	writeJSON(w, http.StatusOK, out)
}

func (a *Admin) backends(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.backendStatuses())
}

func (a *Admin) backendStatuses() []BackendStatus {
	var out []BackendStatus
	for _, b := range a.router.Backends() {
		out = append(out, BackendStatus{
			Provider: b.Provider.Name(), Healthy: b.Healthy(),
			LatencyMs: b.EWMA(), Models: b.Provider.Models(),
		})
	}
	return out
}

func (a *Admin) purge(w http.ResponseWriter, _ *http.Request) {
	if a.cache == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "cache disabled"})
		return
	}
	a.cache.Purge()
	writeJSON(w, http.StatusOK, map[string]string{"status": "purged"})
}
