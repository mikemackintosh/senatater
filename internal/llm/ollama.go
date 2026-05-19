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

const defaultOllamaURL = "http://localhost:11434"

// OllamaClient communicates with an Ollama server's /api/chat endpoint.
type OllamaClient struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

// NewOllama returns a client targeting an Ollama server. Empty baseURL
// defaults to http://localhost:11434.
func NewOllama(model, baseURL string) *OllamaClient {
	if baseURL == "" {
		baseURL = defaultOllamaURL
	}
	return &OllamaClient{
		BaseURL: baseURL,
		Model:   model,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

type ollamaChatOptions struct {
	Temperature float64 `json:"temperature"`
	NumCtx      int     `json:"num_ctx"`
}

type ollamaChatRequest struct {
	Model    string            `json:"model"`
	Messages []Message         `json:"messages"`
	Stream   bool              `json:"stream"`
	Options  ollamaChatOptions `json:"options"`
}

type ollamaChatResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

// Chat streams a response from Ollama's /api/chat (NDJSON), invoking
// onToken for each incremental chunk and returning the assembled string.
func (c *OllamaClient) Chat(ctx context.Context, messages []Message, onToken func(string)) (string, error) {
	body, err := json.Marshal(ollamaChatRequest{
		Model:    c.Model,
		Messages: messages,
		Stream:   true,
		Options:  ollamaChatOptions{Temperature: 0.2, NumCtx: 8192},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama chat: status %d", resp.StatusCode)
	}

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk ollamaChatResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			return "", fmt.Errorf("ollama chat: decode: %w", err)
		}
		if chunk.Message.Content != "" {
			full.WriteString(chunk.Message.Content)
			if onToken != nil {
				onToken(chunk.Message.Content)
			}
		}
		if chunk.Done {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("ollama chat: read: %w", err)
	}
	return full.String(), nil
}
