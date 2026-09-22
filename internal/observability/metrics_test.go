package observability

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func render(r *Registry) string {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	r.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}

func TestCounterAndGaugeExposition(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounterVec("reqs_total", "requests", "code")
	c.Inc("200")
	c.Add(2, "200")
	c.Inc("500")
	g := r.NewGaugeVec("temp", "temperature", "room")
	g.Set(21.5, "office")

	out := render(r)
	if !strings.Contains(out, `reqs_total{code="200"} 3`) {
		t.Fatalf("counter missing/incorrect:\n%s", out)
	}
	if !strings.Contains(out, `reqs_total{code="500"} 1`) {
		t.Fatalf("counter 500 missing:\n%s", out)
	}
	if !strings.Contains(out, `# TYPE reqs_total counter`) {
		t.Fatalf("missing TYPE line:\n%s", out)
	}
	if !strings.Contains(out, `temp{room="office"} 21.5`) {
		t.Fatalf("gauge missing:\n%s", out)
	}
}

func TestHistogramExposition(t *testing.T) {
	r := NewRegistry()
	h := r.NewHistogramVec("dur_seconds", "durations", []float64{0.1, 0.5, 1}, "route")
	h.Observe(0.05, "/a")
	h.Observe(0.4, "/a")
	h.Observe(2.0, "/a")

	out := render(r)
	if !strings.Contains(out, `dur_seconds_bucket{route="/a",le="0.1"} 1`) {
		t.Fatalf("le=0.1 bucket wrong:\n%s", out)
	}
	if !strings.Contains(out, `dur_seconds_bucket{route="/a",le="0.5"} 2`) {
		t.Fatalf("le=0.5 bucket wrong:\n%s", out)
	}
	if !strings.Contains(out, `dur_seconds_bucket{route="/a",le="+Inf"} 3`) {
		t.Fatalf("+Inf bucket wrong:\n%s", out)
	}
	if !strings.Contains(out, `dur_seconds_count{route="/a"} 3`) {
		t.Fatalf("count wrong:\n%s", out)
	}
}
