// Package provider defines the OpenAI-compatible request/response contract and
// the Provider interface that every upstream LLM backend implements.
package provider

import (
	"context"
	"encoding/json"
	"errors"
)

// Message is a single chat message in the OpenAI schema.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is an OpenAI-compatible /v1/chat/completions request.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
	User        string    `json:"user,omitempty"`
}

// Usage reports token accounting for a response.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Choice is a single completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// ChatResponse is an OpenAI-compatible non-streaming response.
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
	// Cached and Provider are Sluice extensions surfaced via response headers,
	// not part of the wire body, but kept here for internal bookkeeping.
	Cached   bool   `json:"-"`
	Provider string `json:"-"`
}

// StreamChoice is a single choice inside a streamed chunk.
type StreamChoice struct {
	Index        int     `json:"index"`
	Delta        Message `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// ChatChunk is one server-sent chunk of a streaming completion.
type ChatChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

// StreamEvent is delivered on the channel returned by Provider.Stream.
type StreamEvent struct {
	Chunk *ChatChunk
	Err   error
	Done  bool
}

// StringList accepts either a JSON string or an array of strings, matching the
// OpenAI embeddings "input" field which is polymorphic.
type StringList []string

// UnmarshalJSON implements json.Unmarshaler for the polymorphic input field.
func (s *StringList) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = []string{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

// EmbedRequest is an OpenAI-compatible /v1/embeddings request.
type EmbedRequest struct {
	Model string     `json:"model"`
	Input StringList `json:"input"`
}

// EmbedData is a single embedding vector.
type EmbedData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// EmbedResponse is an OpenAI-compatible embeddings response.
type EmbedResponse struct {
	Object string      `json:"object"`
	Data   []EmbedData `json:"data"`
	Model  string      `json:"model"`
	Usage  Usage       `json:"usage"`
}

// Model describes an available model for GET /v1/models.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// Provider is an upstream LLM backend. Implementations must be safe for
// concurrent use by multiple goroutines.
type Provider interface {
	// Name is the stable identifier used in config, routing and metrics.
	Name() string
	// Models lists the model IDs this provider serves.
	Models() []string
	// Chat performs a non-streaming completion.
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	// Stream performs a streaming completion, emitting events on the channel
	// until an event with Done or Err is sent, after which the channel closes.
	Stream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error)
	// Embed produces embedding vectors for the given inputs.
	Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error)
}

// ErrModelNotSupported is returned when a provider is asked for a model it does
// not serve.
var ErrModelNotSupported = errors.New("provider: model not supported")

// APIError is an OpenAI-compatible error envelope.
type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string { return e.Message }

// NewAPIError builds an APIError with an HTTP status.
func NewAPIError(status int, typ, msg string) *APIError {
	return &APIError{Message: msg, Type: typ, Status: status}
}
