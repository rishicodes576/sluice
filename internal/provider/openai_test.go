package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAIChat exercises the real HTTP adapter against a stub server, so the
// production code path is covered without any live API key.
func TestOpenAIChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("missing auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmpl-1","object":"chat.completion","model":"gpt","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	o := NewOpenAI("openai", srv.URL, "sk-test", []string{"gpt"}, srv.Client())
	resp, err := o.Chat(context.Background(), &ChatRequest{Model: "gpt", Messages: []Message{{Role: "user", Content: "hey"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Message.Content != "hi" || resp.Provider != "openai" {
		t.Fatalf("unexpected response %+v", resp)
	}
}

func TestOpenAIErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down","type":"rate_limit"}}`))
	}))
	defer srv.Close()
	o := NewOpenAI("openai", srv.URL, "", []string{"gpt"}, srv.Client())
	_, err := o.Chat(context.Background(), &ChatRequest{Model: "gpt", Messages: []Message{{Role: "user", Content: "x"}}})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want mapped APIError 429", err)
	}
}
