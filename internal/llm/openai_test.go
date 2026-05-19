package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAI_Chat_StreamsAndAssembles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Three content deltas, a trailing finish_reason chunk, then [DONE].
		_, _ = w.Write([]byte(
			`data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}` + "\n\n" +
				`data: {"choices":[{"delta":{"content":", "},"finish_reason":null}]}` + "\n\n" +
				`data: {"choices":[{"delta":{"content":"world."},"finish_reason":null}]}` + "\n\n" +
				`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
				`data: [DONE]` + "\n\n",
		))
	}))
	defer srv.Close()

	c := NewOpenAI("any-model", srv.URL, "")
	var tokens []string
	got, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello, world." {
		t.Errorf("assembled: %q", got)
	}
	if strings.Join(tokens, "") != "Hello, world." {
		t.Errorf("streamed tokens: %v", tokens)
	}
	if len(tokens) != 3 {
		t.Errorf("expected 3 token callbacks, got %d", len(tokens))
	}
}

func TestOpenAI_Chat_SendsBearerToken(t *testing.T) {
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewOpenAI("model", srv.URL, "sk-test-token")
	if _, err := c.Chat(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test-token" {
		t.Errorf("Authorization header: %q", gotAuth)
	}
}

func TestOpenAI_Chat_OmitsBearerWhenKeyEmpty(t *testing.T) {
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewOpenAI("model", srv.URL, "")
	if _, err := c.Chat(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestOpenAI_Chat_ErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewOpenAI("model", srv.URL, "")
	if _, err := c.Chat(context.Background(), nil, nil); err == nil {
		t.Error("expected error on non-200")
	}
}

func TestNew_FactorySelectsBackend(t *testing.T) {
	if _, ok := New(BackendOllama, "m", "", "").(*OllamaClient); !ok {
		t.Error("ollama factory did not return *OllamaClient")
	}
	if _, ok := New(BackendOpenAI, "m", "", "").(*OpenAIClient); !ok {
		t.Error("openai factory did not return *OpenAIClient")
	}
	if _, ok := New(Backend("unknown"), "m", "", "").(*OllamaClient); !ok {
		t.Error("unknown backend should default to ollama")
	}
}
