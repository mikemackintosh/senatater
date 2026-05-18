package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client communicates with a local Ollama server for embeddings.
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
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed produces a vector embedding for each input string in a single request.
func (c *Client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: c.Model, Input: inputs})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed: status %d", resp.StatusCode)
	}
	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("ollama embed: got %d embeddings for %d inputs", len(out.Embeddings), len(inputs))
	}
	return out.Embeddings, nil
}
