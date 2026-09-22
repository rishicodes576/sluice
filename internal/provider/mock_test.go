package provider

import (
	"context"
	"strings"
	"testing"
)

func TestMockChatDeterministic(t *testing.T) {
	m := NewMock("mock", []string{"m"})
	req := &ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hello there"}}}
	a, err := m.Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := m.Chat(context.Background(), req)
	if a.Choices[0].Message.Content != b.Choices[0].Message.Content {
		t.Fatal("mock completions should be deterministic")
	}
	if a.Usage.TotalTokens == 0 {
		t.Fatal("expected non-zero usage")
	}
}

func TestMockUnsupportedModel(t *testing.T) {
	m := NewMock("mock", []string{"m"})
	if _, err := m.Chat(context.Background(), &ChatRequest{Model: "nope", Messages: []Message{{Role: "user", Content: "x"}}}); err != ErrModelNotSupported {
		t.Fatalf("err = %v, want ErrModelNotSupported", err)
	}
}

func TestMockStream(t *testing.T) {
	m := NewMock("mock", []string{"m"})
	ch, err := m.Stream(context.Background(), &ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "one two three"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	done := false
	for ev := range ch {
		if ev.Err != nil {
			t.Fatal(ev.Err)
		}
		if ev.Done {
			done = true
			continue
		}
		if ev.Chunk != nil && len(ev.Chunk.Choices) > 0 {
			got.WriteString(ev.Chunk.Choices[0].Delta.Content)
		}
	}
	if !done {
		t.Fatal("stream never signalled done")
	}
	if !strings.Contains(got.String(), "one") {
		t.Fatalf("assembled stream missing content: %q", got.String())
	}
}

func TestMockEmbedSimilarity(t *testing.T) {
	m := NewMock("mock", []string{"m"})
	resp, err := m.Embed(context.Background(), &EmbedRequest{Model: "m", Input: []string{"cats and dogs", "cats and dogs"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("got %d, want 2", len(resp.Data))
	}
	// Identical inputs must yield identical vectors.
	for i := range resp.Data[0].Embedding {
		if resp.Data[0].Embedding[i] != resp.Data[1].Embedding[i] {
			t.Fatal("identical inputs produced different embeddings")
		}
	}
}
