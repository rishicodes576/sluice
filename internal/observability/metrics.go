// Package observability provides structured logging, lightweight tracing and a
// dependency-free, Prometheus-compatible metrics registry. Implementing the
// text exposition format directly keeps the gateway's core free of third-party
// modules while remaining scrape-compatible with Prometheus/Grafana.
package observability

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Registry holds all metrics and renders them in the Prometheus text format.
type Registry struct {
	mu      sync.RWMutex
	metrics []renderable
}

type renderable interface {
	render(sb *strings.Builder)
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) register(m renderable) {
	r.mu.Lock()
	r.metrics = append(r.metrics, m)
	r.mu.Unlock()
}

// Handler serves the metrics in Prometheus exposition format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var sb strings.Builder
		r.mu.RLock()
		for _, m := range r.metrics {
			m.render(&sb)
		}
		r.mu.RUnlock()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(sb.String()))
	})
}

// labelKey joins label values in declared order into a stable series key.
func labelKey(vals []string) string { return strings.Join(vals, "\x1f") }

func writeLabels(sb *strings.Builder, names, vals []string) {
	if len(names) == 0 {
		return
	}
	sb.WriteByte('{')
	for i, n := range names {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(n)
		sb.WriteString(`="`)
		sb.WriteString(escape(vals[i]))
		sb.WriteByte('"')
	}
	sb.WriteByte('}')
}

func escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// CounterVec is a set of monotonically increasing counters partitioned by
// label values.
type CounterVec struct {
	name, help string
	labels     []string
	mu         sync.Mutex
	series     map[string]*counterSeries
}

type counterSeries struct {
	vals []string
	v    float64
}

// NewCounterVec registers and returns a labelled counter.
func (r *Registry) NewCounterVec(name, help string, labels ...string) *CounterVec {
	c := &CounterVec{name: name, help: help, labels: labels, series: map[string]*counterSeries{}}
	r.register(c)
	return c
}

// Inc increments the counter for the given label values by 1.
func (c *CounterVec) Inc(labelVals ...string) { c.Add(1, labelVals...) }

// Add increments the counter for the given label values by delta.
func (c *CounterVec) Add(delta float64, labelVals ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := labelKey(labelVals)
	s, ok := c.series[k]
	if !ok {
		s = &counterSeries{vals: append([]string(nil), labelVals...)}
		c.series[k] = s
	}
	s.v += delta
}

func (c *CounterVec) render(sb *strings.Builder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(sb, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
	for _, s := range sortedCounter(c.series) {
		sb.WriteString(c.name)
		writeLabels(sb, c.labels, s.vals)
		sb.WriteByte(' ')
		sb.WriteString(strconv.FormatFloat(s.v, 'g', -1, 64))
		sb.WriteByte('\n')
	}
}

// GaugeVec is a set of arbitrary-valued gauges partitioned by label values.
type GaugeVec struct {
	name, help string
	labels     []string
	mu         sync.Mutex
	series     map[string]*counterSeries
}

// NewGaugeVec registers and returns a labelled gauge.
func (r *Registry) NewGaugeVec(name, help string, labels ...string) *GaugeVec {
	g := &GaugeVec{name: name, help: help, labels: labels, series: map[string]*counterSeries{}}
	r.register(g)
	return g
}

// Set sets the gauge value for the given label values.
func (g *GaugeVec) Set(v float64, labelVals ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	k := labelKey(labelVals)
	s, ok := g.series[k]
	if !ok {
		s = &counterSeries{vals: append([]string(nil), labelVals...)}
		g.series[k] = s
	}
	s.v = v
}

func (g *GaugeVec) render(sb *strings.Builder) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fmt.Fprintf(sb, "# HELP %s %s\n# TYPE %s gauge\n", g.name, g.help, g.name)
	for _, s := range sortedCounter(g.series) {
		sb.WriteString(g.name)
		writeLabels(sb, g.labels, s.vals)
		sb.WriteByte(' ')
		sb.WriteString(strconv.FormatFloat(s.v, 'g', -1, 64))
		sb.WriteByte('\n')
	}
}

// HistogramVec is a set of cumulative histograms partitioned by label values.
type HistogramVec struct {
	name, help string
	labels     []string
	buckets    []float64
	mu         sync.Mutex
	series     map[string]*histSeries
}

type histSeries struct {
	vals   []string
	counts []uint64
	sum    float64
	count  uint64
}

// DefaultBuckets are latency buckets in seconds suited to LLM request timing.
var DefaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// NewHistogramVec registers and returns a labelled histogram.
func (r *Registry) NewHistogramVec(name, help string, buckets []float64, labels ...string) *HistogramVec {
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	h := &HistogramVec{name: name, help: help, labels: labels, buckets: buckets, series: map[string]*histSeries{}}
	r.register(h)
	return h
}

// Observe records a value (e.g. request duration in seconds).
func (h *HistogramVec) Observe(v float64, labelVals ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := labelKey(labelVals)
	s, ok := h.series[k]
	if !ok {
		s = &histSeries{vals: append([]string(nil), labelVals...), counts: make([]uint64, len(h.buckets))}
		h.series[k] = s
	}
	s.sum += v
	s.count++
	for i, ub := range h.buckets {
		if v <= ub {
			s.counts[i]++
		}
	}
}

func (h *HistogramVec) render(sb *strings.Builder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Fprintf(sb, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := h.series[k]
		for i, ub := range h.buckets {
			names := append(append([]string(nil), h.labels...), "le")
			vals := append(append([]string(nil), s.vals...), strconv.FormatFloat(ub, 'g', -1, 64))
			sb.WriteString(h.name + "_bucket")
			writeLabels(sb, names, vals)
			fmt.Fprintf(sb, " %d\n", s.counts[i])
		}
		infNames := append(append([]string(nil), h.labels...), "le")
		infVals := append(append([]string(nil), s.vals...), "+Inf")
		sb.WriteString(h.name + "_bucket")
		writeLabels(sb, infNames, infVals)
		fmt.Fprintf(sb, " %d\n", s.count)
		sb.WriteString(h.name + "_sum")
		writeLabels(sb, h.labels, s.vals)
		fmt.Fprintf(sb, " %s\n", strconv.FormatFloat(s.sum, 'g', -1, 64))
		sb.WriteString(h.name + "_count")
		writeLabels(sb, h.labels, s.vals)
		fmt.Fprintf(sb, " %d\n", s.count)
	}
}

func sortedCounter(m map[string]*counterSeries) []*counterSeries {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*counterSeries, len(keys))
	for i, k := range keys {
		out[i] = m[k]
	}
	return out
}
