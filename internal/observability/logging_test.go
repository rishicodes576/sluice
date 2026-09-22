package observability

import (
	"context"
	"testing"
)

func TestNewLoggerLevels(t *testing.T) {
	for _, lvl := range []string{"debug", "info", "warn", "error", "unknown"} {
		if NewLogger(lvl, "json") == nil {
			t.Fatalf("nil logger for level %q", lvl)
		}
	}
	if NewLogger("info", "text") == nil {
		t.Fatal("nil text logger")
	}
}

func TestTraceRoundTrip(t *testing.T) {
	id := NewTraceID()
	if len(id) != 32 {
		t.Fatalf("trace id length = %d, want 32 hex chars", len(id))
	}
	ctx := WithTrace(context.Background(), id)
	if TraceID(ctx) != id {
		t.Fatal("trace id did not round-trip through context")
	}
	if TraceID(context.Background()) != "" {
		t.Fatal("empty context should have no trace id")
	}
}
