package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/rishicodes576/sluice/internal/observability"
)

// Middleware wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// chain applies middleware so the first listed runs outermost.
func chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush proxies to the underlying flusher so SSE streaming still works.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// recoverMW converts panics into 500s instead of crashing the process.
func recoverMW(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered", "err", rec, "path", r.URL.Path)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":{"message":"internal error","type":"server_error"}}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// traceMW assigns/propagates a trace id and echoes it as a response header.
func traceMW() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Trace-Id")
			if id == "" {
				id = observability.NewTraceID()
			}
			w.Header().Set("X-Trace-Id", id)
			next.ServeHTTP(w, r.WithContext(observability.WithTrace(r.Context(), id)))
		})
	}
}

// logMW emits a structured access log per request.
func logMW(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			log.Info("request",
				"method", r.Method, "path", r.URL.Path, "status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"trace_id", observability.TraceID(r.Context()),
			)
		})
	}
}
