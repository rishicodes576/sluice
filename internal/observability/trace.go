package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type traceCtxKey struct{}

// NewTraceID returns a random 16-byte hex trace id.
func NewTraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}

// WithTrace stores a trace id in the context.
func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceCtxKey{}, id)
}

// TraceID returns the context's trace id, or "" if none.
func TraceID(ctx context.Context) string {
	if v, ok := ctx.Value(traceCtxKey{}).(string); ok {
		return v
	}
	return ""
}
