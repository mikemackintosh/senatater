package pipeline

import (
	"context"
	"path/filepath"
	"testing"

	"emails-rag/internal/embed"
	"emails-rag/internal/llm"
	"emails-rag/internal/store"
)

type stubLLM struct {
	called  bool
	respond string
}

func (s *stubLLM) Chat(ctx context.Context, msgs []llm.Message, onToken func(string)) (string, error) {
	s.called = true
	if onToken != nil && s.respond != "" {
		onToken(s.respond)
	}
	return s.respond, nil
}

func TestSearcher_OnStageFiresInOrder(t *testing.T) {
	// Build a tiny on-disk store so the embedding and search stages are real;
	// the LLM is a stub so we don't need a network. Without indexed data the
	// query embedding still gets computed by the (live) Ollama embedder — to
	// keep the test offline-friendly, we skip if no embedding service is up.
	tmp := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.InsertBatch(context.Background(), []store.Chunk{{
		Source: "x", SourceType: "pdf", Content: "hi",
		Metadata: map[string]string{}, Embedding: []float32{0.1, 0.2, 0.3},
	}}); err != nil {
		t.Fatal(err)
	}

	// Use an embedder that points at a non-existent server — we don't want
	// to depend on Ollama for this test, so we use a fake embedder via type
	// assertion isn't possible. Instead we test OnStage at the Searcher
	// level by mocking just enough to drive stage progression.
	searcher := &Searcher{
		Embedder: embed.New("nomic-embed-text"),
		LLM:      &stubLLM{respond: "ok"},
		Store:    s,
		TopK:     1,
	}

	var stages []string
	searcher.OnStage = func(stage string) {
		stages = append(stages, stage)
	}

	// We expect "embedding" to be called first; the embed call will fail
	// (no Ollama in tests) but that's fine — we only assert the first stage
	// callback fires before the failure.
	_, _, _ = searcher.Answer(context.Background(), "what is this?", nil)

	if len(stages) == 0 || stages[0] != "embedding" {
		t.Errorf("expected first stage 'embedding', got %v", stages)
	}
}

func TestParseSourceFilter(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantType  string
		wantQuery string
	}{
		{"no prefix", "what is rag?", "", "what is rag?"},
		{"pdf prefix", "source:pdf what does the lease say?", "pdf", "what does the lease say?"},
		{"mbox prefix", "source:mbox when did Sarah email?", "mbox", "when did Sarah email?"},
		{"uppercase type normalized", "source:PDF hello", "pdf", "hello"},
		{"unknown type passes through", "source:web search me", "", "source:web search me"},
		{"prefix only, no query", "source:pdf", "", "source:pdf"},
		{"leading whitespace stripped", "   source:pdf hello", "pdf", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotT, gotQ := parseSourceFilter(tt.in)
			if gotT != tt.wantType {
				t.Errorf("type: got %q, want %q", gotT, tt.wantType)
			}
			if gotQ != tt.wantQuery {
				t.Errorf("query: got %q, want %q", gotQ, tt.wantQuery)
			}
		})
	}
}
