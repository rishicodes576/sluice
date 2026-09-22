package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAI is a real adapter for any OpenAI-compatible HTTP endpoint (OpenAI,
// Azure OpenAI, together.ai, groq, local vLLM, etc.). It is exercised in tests
// via httptest rather than live keys.
type OpenAI struct {
	name    string
	baseURL string
	apiKey  string
	models  []string
	http    *http.Client
}

// NewOpenAI creates an OpenAI-compatible provider. baseURL defaults to the
// public OpenAI API when empty.
func NewOpenAI(name, baseURL, apiKey string, models []string, hc *http.Client) *OpenAI {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &OpenAI{name: name, baseURL: baseURL, apiKey: apiKey, models: models, http: hc}
}

// Name implements Provider.
func (o *OpenAI) Name() string { return o.name }

// Models implements Provider.
func (o *OpenAI) Models() []string { return o.models }

func (o *OpenAI) newReq(ctx context.Context, path string, body any) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	return req, nil
}

func decodeError(status int, body []byte) error {
	var env struct {
		Error APIError `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		env.Error.Status = status
		return &env.Error
	}
	return NewAPIError(status, "upstream_error", fmt.Sprintf("upstream returned %d: %s", status, truncate(body, 256)))
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}

// Chat implements Provider.
func (o *OpenAI) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	r := *req
	r.Stream = false
	httpReq, err := o.newReq(ctx, "/chat/completions", &r)
	if err != nil {
		return nil, err
	}
	resp, err := o.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, decodeError(resp.StatusCode, body)
	}
	var out ChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	out.Provider = o.name
	return &out, nil
}

// Stream implements Provider by parsing the upstream SSE token stream.
func (o *OpenAI) Stream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error) {
	r := *req
	r.Stream = true
	httpReq, err := o.newReq(ctx, "/chat/completions", &r)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := o.http.Do(httpReq)
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
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				ch <- StreamEvent{Done: true}
				return
			}
			var chunk ChatChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue
			}
			select {
			case <-ctx.Done():
				ch <- StreamEvent{Err: ctx.Err()}
				return
			case ch <- StreamEvent{Chunk: &chunk}:
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

// Embed implements Provider.
func (o *OpenAI) Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error) {
	httpReq, err := o.newReq(ctx, "/embeddings", req)
	if err != nil {
		return nil, err
	}
	resp, err := o.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, decodeError(resp.StatusCode, body)
	}
	var out EmbedResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
