package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// Anthropic adapts the Messages API to the OpenAI-compatible surface Sluice
// exposes, translating request/response shapes in both directions. Like the
// OpenAI adapter it is real code, tested via httptest rather than live keys.
type Anthropic struct {
	name    string
	baseURL string
	apiKey  string
	version string
	models  []string
	http    *http.Client
}

// NewAnthropic creates an Anthropic provider.
func NewAnthropic(name, baseURL, apiKey string, models []string, hc *http.Client) *Anthropic {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Anthropic{name: name, baseURL: baseURL, apiKey: apiKey, version: "2023-06-01", models: models, http: hc}
}

// Name implements Provider.
func (a *Anthropic) Name() string { return a.name }

// Models implements Provider.
func (a *Anthropic) Models() []string { return a.models }

type anthropicReq struct {
	Model     string    `json:"model"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream,omitempty"`
}

// toAnthropic splits out any system message (Anthropic carries it separately).
func toAnthropic(req *ChatRequest) anthropicReq {
	out := anthropicReq{Model: req.Model, MaxTokens: req.MaxTokens, Stream: req.Stream}
	if out.MaxTokens == 0 {
		out.MaxTokens = 1024
	}
	for _, m := range req.Messages {
		if m.Role == "system" {
			if out.System != "" {
				out.System += "\n"
			}
			out.System += m.Content
			continue
		}
		out.Messages = append(out.Messages, m)
	}
	return out
}

func (a *Anthropic) newReq(ctx context.Context, body any) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", a.version)
	if a.apiKey != "" {
		req.Header.Set("x-api-key", a.apiKey)
	}
	return req, nil
}

// Chat implements Provider.
func (a *Anthropic) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	httpReq, err := a.newReq(ctx, toAnthropic(req))
	if err != nil {
		return nil, err
	}
	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, decodeError(resp.StatusCode, body)
	}
	var out struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var text strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return &ChatResponse{
		ID:      out.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   out.Model,
		Choices: []Choice{{Index: 0, Message: Message{Role: "assistant", Content: text.String()}, FinishReason: mapStop(out.StopReason)}},
		Usage: Usage{
			PromptTokens:     out.Usage.InputTokens,
			CompletionTokens: out.Usage.OutputTokens,
			TotalTokens:      out.Usage.InputTokens + out.Usage.OutputTokens,
		},
		Provider: a.name,
	}, nil
}

func mapStop(s string) string {
	switch s {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	default:
		return s
	}
}

// Stream implements Provider by translating Anthropic SSE events into
// OpenAI-style chunks.
func (a *Anthropic) Stream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	ar := toAnthropic(req)
	ar.Stream = true
	httpReq, err := a.newReq(ctx, ar)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := a.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, decodeError(resp.StatusCode, body)
	}
	ch := make(chan StreamEvent)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		created := time.Now().Unix()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var evt struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &evt) != nil {
				continue
			}
			switch evt.Type {
			case "content_block_delta":
				chunk := &ChatChunk{
					Object: "chat.completion.chunk", Created: created, Model: req.Model,
					Choices: []StreamChoice{{Index: 0, Delta: Message{Content: evt.Delta.Text}}},
				}
				select {
				case <-ctx.Done():
					ch <- StreamEvent{Err: ctx.Err()}
					return
				case ch <- StreamEvent{Chunk: chunk}:
				}
			case "message_stop":
				ch <- StreamEvent{Done: true}
				return
			}
		}
		if err := sc.Err(); err != nil {
			ch <- StreamEvent{Err: err}
			return
		}
		ch <- StreamEvent{Done: true}
	}()
	return ch, nil
}

// Embed implements Provider. Anthropic has no first-party embeddings endpoint,
// so this returns a clear, typed error that the caller can route around.
func (a *Anthropic) Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error) {
	return nil, NewAPIError(http.StatusNotImplemented, "unsupported", "anthropic provider does not support embeddings")
}
