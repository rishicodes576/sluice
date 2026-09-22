package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicChatTranslation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "ak-test" {
			t.Errorf("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Errorf("missing anthropic-version header")
		}
		// System message must be split out of messages.
		var req anthropicReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.System != "be brief" {
			t.Errorf("system = %q, want 'be brief'", req.System)
		}
		if len(req.Messages) != 1 {
			t.Errorf("messages = %d, want 1 (system removed)", len(req.Messages))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"claude","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`))
	}))
	defer srv.Close()

	a := NewAnthropic("anthropic", srv.URL, "ak-test", []string{"claude"}, srv.Client())
	resp, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude",
		Messages: []Message{
			{Role: "system", Content: "be brief"},
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("content = %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason = %q, want stop (mapped from end_turn)", resp.Choices[0].FinishReason)
	}
	if resp.Usage.TotalTokens != 5 {
		t.Fatalf("total tokens = %d, want 5", resp.Usage.TotalTokens)
	}
}

func TestAnthropicStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: content_block_delta\n" +
				`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}` + "\n\n" +
				`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}` + "\n\n" +
				`data: {"type":"message_stop"}` + "\n\n"))
	}))
	defer srv.Close()

	a := NewAnthropic("anthropic", srv.URL, "ak", []string{"claude"}, srv.Client())
	ch, err := a.Stream(context.Background(), &ChatRequest{Model: "claude", Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for ev := range ch {
		if ev.Chunk != nil && len(ev.Chunk.Choices) > 0 {
			got.WriteString(ev.Chunk.Choices[0].Delta.Content)
		}
	}
	if got.String() != "Hello" {
		t.Fatalf("assembled stream = %q, want Hello", got.String())
	}
}

func TestAnthropicChatError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()
	a := NewAnthropic("anthropic", srv.URL, "", []string{"claude"}, srv.Client())
	_, err := a.Chat(context.Background(), &ChatRequest{Model: "claude", Messages: []Message{{Role: "user", Content: "x"}}})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want APIError 400", err)
	}
}

func TestAnthropicEmbedUnsupported(t *testing.T) {
	a := NewAnthropic("anthropic", "http://unused", "", []string{"claude"}, nil)
	_, err := a.Embed(context.Background(), &EmbedRequest{Model: "claude", Input: []string{"x"}})
	if err == nil {
		t.Fatal("expected embeddings to be unsupported")
	}
}

func TestMapStop(t *testing.T) {
	cases := map[string]string{"end_turn": "stop", "stop_sequence": "stop", "max_tokens": "length", "other": "other"}
	for in, want := range cases {
		if got := mapStop(in); got != want {
			t.Errorf("mapStop(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStringListUnmarshal(t *testing.T) {
	var s StringList
	if err := json.Unmarshal([]byte(`"solo"`), &s); err != nil || len(s) != 1 || s[0] != "solo" {
		t.Fatalf("single string: %v %v", s, err)
	}
	if err := json.Unmarshal([]byte(`["a","b"]`), &s); err != nil || len(s) != 2 {
		t.Fatalf("array: %v %v", s, err)
	}
}
