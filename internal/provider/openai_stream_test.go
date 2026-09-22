package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIStreamParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			`data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hel"}}]}` + "\n\n" +
				`data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"lo"}}]}` + "\n\n" +
				"data: [DONE]\n\n"))
	}))
	defer srv.Close()

	o := NewOpenAI("openai", srv.URL, "sk", []string{"gpt"}, srv.Client())
	ch, err := o.Stream(context.Background(), &ChatRequest{Model: "gpt", Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	done := false
	for ev := range ch {
		if ev.Done {
			done = true
		}
		if ev.Chunk != nil && len(ev.Chunk.Choices) > 0 {
			got.WriteString(ev.Chunk.Choices[0].Delta.Content)
		}
	}
	if !done || got.String() != "Hello" {
		t.Fatalf("stream = %q done=%v", got.String(), done)
	}
}

func TestOpenAIStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"no key","type":"auth"}}`))
	}))
	defer srv.Close()
	o := NewOpenAI("openai", srv.URL, "", []string{"gpt"}, srv.Client())
	_, err := o.Stream(context.Background(), &ChatRequest{Model: "gpt", Messages: []Message{{Role: "user", Content: "x"}}})
	if apiErr, ok := err.(*APIError); !ok || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v, want APIError 401", err)
	}
}

func TestOpenAIEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","model":"emb","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	defer srv.Close()
	o := NewOpenAI("openai", srv.URL, "sk", []string{"emb"}, srv.Client())
	resp, err := o.Embed(context.Background(), &EmbedRequest{Model: "emb", Input: []string{"hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || len(resp.Data[0].Embedding) != 2 {
		t.Fatalf("unexpected embed response %+v", resp)
	}
}
