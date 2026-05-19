package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultOpenAIURL = "http://localhost:8000"

// OpenAIClient talks to any OpenAI-compatible /v1/chat/completions
// endpoint — vLLM, LM Studio, the actual OpenAI API — using Server-Sent
// Events for streaming.
type OpenAIClient struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
}

// NewOpenAI builds a client. baseURL is the server root (the client
// appends /v1/chat/completions). apiKey is optional.
func NewOpenAI(model, baseURL, apiKey string) *OpenAIClient {
	if baseURL == "" {
		baseURL = defaultOpenAIURL
	}
	return &OpenAIClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

type openAIChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature float64   `json:"temperature"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// Chat streams a completion via SSE. Lines come in as `data: {json}` with
// a terminating `data: [DONE]`; per-chunk delta.content is accumulated.
func (c *OpenAIClient) Chat(ctx context.Context, messages []Message, onToken func(string)) (string, error) {
	body, err := json.Marshal(openAIChatRequest{
		Model:       c.Model,
		Messages:    messages,
		Stream:      true,
		Temperature: 0.2,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai chat: status %d", resp.StatusCode)
	}

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if bytes.Equal(data, []byte("[DONE]")) {
			break
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return "", fmt.Errorf("openai chat: decode: %w", err)
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content == "" {
				continue
			}
			full.WriteString(ch.Delta.Content)
			if onToken != nil {
				onToken(ch.Delta.Content)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("openai chat: read: %w", err)
	}
	return full.String(), nil
}
