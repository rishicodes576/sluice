package provider

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

// Mock is a deterministic, dependency-free provider used for local demos, tests
// and CI. It never performs network I/O, so the entire gateway is runnable with
// zero API keys while still exercising every code path (streaming, usage
// accounting, embeddings, simulated latency and failures).
type Mock struct {
	name      string
	models    []string
	latency   time.Duration
	failEvery int // if >0, every Nth call returns a synthetic upstream error
	calls     int
	dim       int
}

// MockOption configures a Mock provider.
type MockOption func(*Mock)

// WithLatency sets a fixed simulated upstream latency.
func WithLatency(d time.Duration) MockOption { return func(m *Mock) { m.latency = d } }

// WithFailEvery makes every Nth call fail, to exercise retries and breakers.
func WithFailEvery(n int) MockOption { return func(m *Mock) { m.failEvery = n } }

// NewMock creates a Mock provider serving the given models.
func NewMock(name string, models []string, opts ...MockOption) *Mock {
	if len(models) == 0 {
		models = []string{"mock-1"}
	}
	m := &Mock{name: name, models: models, dim: 256}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Name implements Provider.
func (m *Mock) Name() string { return m.name }

// Models implements Provider.
func (m *Mock) Models() []string { return m.models }

func (m *Mock) serves(model string) bool {
	for _, x := range m.models {
		if x == model {
			return true
		}
	}
	return false
}

// completion produces a deterministic reply derived from the last user message,
// so identical prompts yield identical outputs (useful for cache assertions).
func (m *Mock) completion(req *ChatRequest) string {
	var last string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			last = req.Messages[i].Content
			break
		}
	}
	if last == "" && len(req.Messages) > 0 {
		last = req.Messages[len(req.Messages)-1].Content
	}
	words := strings.Fields(last)
	if len(words) > 8 {
		words = words[:8]
	}
	return fmt.Sprintf("[%s] Echoing %q via model %s.", m.name, strings.Join(words, " "), req.Model)
}

func countTokens(s string) int {
	n := len(strings.Fields(s))
	if n == 0 && s != "" {
		n = 1
	}
	return n
}

func (m *Mock) simulate(ctx context.Context) error {
	m.calls++
	if m.failEvery > 0 && m.calls%m.failEvery == 0 {
		return NewAPIError(502, "upstream_error", "mock: simulated upstream failure")
	}
	if m.latency <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(m.latency):
		return nil
	}
}

// Chat implements Provider.
func (m *Mock) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if !m.serves(req.Model) {
		return nil, ErrModelNotSupported
	}
	if err := m.simulate(ctx); err != nil {
		return nil, err
	}
	text := m.completion(req)
	pt := 0
	for _, msg := range req.Messages {
		pt += countTokens(msg.Content)
	}
	ct := countTokens(text)
	return &ChatResponse{
		ID:      "chatcmpl-mock-" + hashHex(text),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []Choice{{
			Index:        0,
			Message:      Message{Role: "assistant", Content: text},
			FinishReason: "stop",
		}},
		Usage:    Usage{PromptTokens: pt, CompletionTokens: ct, TotalTokens: pt + ct},
		Provider: m.name,
	}, nil
}

// Stream implements Provider by chunking the deterministic completion word by
// word, mirroring the OpenAI streaming wire format.
func (m *Mock) Stream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	if !m.serves(req.Model) {
		return nil, ErrModelNotSupported
	}
	if err := m.simulate(ctx); err != nil {
		return nil, err
	}
	text := m.completion(req)
	id := "chatcmpl-mock-" + hashHex(text)
	created := time.Now().Unix()
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		words := strings.Fields(text)
		for i, w := range words {
			token := w
			if i < len(words)-1 {
				token += " "
			}
			chunk := &ChatChunk{
				ID: id, Object: "chat.completion.chunk", Created: created, Model: req.Model,
				Choices: []StreamChoice{{Index: 0, Delta: Message{Content: token}}},
			}
			select {
			case <-ctx.Done():
				ch <- StreamEvent{Err: ctx.Err()}
				return
			case ch <- StreamEvent{Chunk: chunk}:
			}
		}
		stop := "stop"
		ch <- StreamEvent{Chunk: &ChatChunk{
			ID: id, Object: "chat.completion.chunk", Created: created, Model: req.Model,
			Choices: []StreamChoice{{Index: 0, FinishReason: &stop}},
		}}
		ch <- StreamEvent{Done: true}
	}()
	return ch, nil
}

// Embed implements Provider with a deterministic hashing embedding, so semantic
// similarity is stable and reproducible without an external embedding service.
func (m *Mock) Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error) {
	if err := m.simulate(ctx); err != nil {
		return nil, err
	}
	data := make([]EmbedData, len(req.Input))
	total := 0
	for i, in := range req.Input {
		data[i] = EmbedData{Object: "embedding", Index: i, Embedding: hashEmbed(in, m.dim)}
		total += countTokens(in)
	}
	return &EmbedResponse{
		Object: "list", Data: data, Model: req.Model,
		Usage: Usage{PromptTokens: total, TotalTokens: total},
	}, nil
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:6])
}

// hashEmbed maps text to a normalized vector via feature hashing over token
// bigrams. Similar text produces similar vectors, which is enough to
// demonstrate semantic caching deterministically and offline.
func hashEmbed(s string, dim int) []float64 {
	v := make([]float64, dim)
	toks := strings.Fields(strings.ToLower(s))
	add := func(feat string) {
		h := sha256.Sum256([]byte(feat))
		idx := binary.BigEndian.Uint32(h[:4]) % uint32(dim)
		sign := 1.0
		if h[4]&1 == 1 {
			sign = -1.0
		}
		v[idx] += sign
	}
	for i, t := range toks {
		add(t)
		if i > 0 {
			add(toks[i-1] + " " + t)
		}
	}
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	for i := range v {
		v[i] /= norm
	}
	return v
}
