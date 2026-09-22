package proxy

import "github.com/rishicodes576/sluice/internal/observability"

// Metrics bundles the Prometheus metrics emitted by the proxy layer.
type Metrics struct {
	Requests  *observability.CounterVec   // endpoint, model, provider, status
	Latency   *observability.HistogramVec // endpoint, model
	Tokens    *observability.CounterVec   // model, kind
	CacheReq  *observability.CounterVec   // model, result
	CostUSD   *observability.CounterVec   // model
	UpstreamL *observability.GaugeVec     // provider
}

// NewMetrics registers and returns the proxy metric set.
func NewMetrics(reg *observability.Registry) *Metrics {
	return &Metrics{
		Requests:  reg.NewCounterVec("sluice_requests_total", "Total API requests.", "endpoint", "model", "provider", "status"),
		Latency:   reg.NewHistogramVec("sluice_request_duration_seconds", "Request duration in seconds.", nil, "endpoint", "model"),
		Tokens:    reg.NewCounterVec("sluice_tokens_total", "Total tokens processed.", "model", "kind"),
		CacheReq:  reg.NewCounterVec("sluice_cache_requests_total", "Semantic cache lookups by result.", "model", "result"),
		CostUSD:   reg.NewCounterVec("sluice_cost_usd_total", "Estimated upstream cost in USD.", "model"),
		UpstreamL: reg.NewGaugeVec("sluice_upstream_latency_ms", "Smoothed (EWMA) upstream latency in ms.", "provider"),
	}
}
