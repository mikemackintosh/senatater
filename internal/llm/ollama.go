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

// Client communicates with a local Ollama server for chat completions.
type Client struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

// New returns a Client configured for the standard local Ollama endpoint.
func New(model string) *Client {
	return &Client{
		BaseURL: "http://localhost:11434",
		Model:   model,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// Message represents one turn in a chat conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature float64 `json:"temperature"`
	NumCtx      int     `json:"num_ctx"`
}

type chatRequest struct {
	Model    string      `json:"model"`
	Messages []Message   `json:"messages"`
	Stream   bool        `json:"stream"`
	Options  chatOptions `json:"options"`
}

type chatResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

// Chat streams an assistant response from Ollama, invoking onToken for each
// incremental chunk (typically a token or short word). The full assembled
// response is also returned. If onToken is nil, tokens are still consumed but
// only the final assembled string is delivered.
func (c *Client) Chat(ctx context.Context, messages []Message, onToken func(string)) (string, error) {
	r := chatRequest{
		Model:    c.Model,
		Messages: messages,
		Stream:   true,
		Options:  chatOptions{Temperature: 0.2, NumCtx: 8192},
	}
	body, err := json.Marshal(r)
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
		var chunk chatResponse
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
