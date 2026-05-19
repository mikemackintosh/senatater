// Package llm provides chat-completion clients for the RAG pipeline.
// Two backends are supported: a local Ollama server and any
// OpenAI-compatible endpoint (vLLM, LM Studio, the OpenAI API itself).
package llm

import "context"

// Message represents one turn in a chat conversation. Wire-compatible
// with both Ollama's /api/chat and OpenAI's /v1/chat/completions.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client is the contract for any chat backend the searcher can call.
// Implementations stream incremental output to onToken (if non-nil) and
// return the full assembled response. Cancelling the context aborts the
// in-flight HTTP request.
type Client interface {
	Chat(ctx context.Context, messages []Message, onToken func(string)) (string, error)
}

// Backend identifies a chat API style. Add new values here when adding
// backends; the factory in New switches on these.
type Backend string

const (
	BackendOllama Backend = "ollama"
	BackendOpenAI Backend = "openai" // also vLLM, LM Studio, etc.
)

// New returns a chat client for the named backend. baseURL is the server
// root (do NOT include "/v1" for the OpenAI variant — the client appends
// the correct path). An empty baseURL uses each backend's default
// (localhost:11434 for Ollama, localhost:8000 for OpenAI). apiKey is only
// used by the OpenAI backend and may be left empty for unauthenticated
// servers like a local vLLM instance.
func New(backend Backend, model, baseURL, apiKey string) Client {
	switch backend {
	case BackendOpenAI:
		return NewOpenAI(model, baseURL, apiKey)
	default:
		return NewOllama(model, baseURL)
	}
}
