package pipeline

import (
	"context"
	"fmt"
	"strings"

	"emails-rag/internal/embed"
	"emails-rag/internal/llm"
	"emails-rag/internal/store"
)

const systemPrompt = `You answer questions using only the provided context.
If the context does not contain enough information, say so plainly.
Cite sources inline by their numeric reference, like [1] or [3].`

// Searcher orchestrates retrieval and generation for a single query.
type Searcher struct {
	Embedder   *embed.Client
	LLM        llm.Client
	Store      *store.Store
	TopK       int
	SourceType string // default filter; "" means all types

	// OnStage, if set, is invoked at the start of each pipeline stage
	// ("embedding", "searching", "generating") so the CLI can show the user
	// what's currently blocking. Stages always run in that order.
	OnStage func(stage string)
}

// NewSearcher returns a Searcher with default top-k retrieval.
func NewSearcher(e *embed.Client, l llm.Client, s *store.Store) *Searcher {
	return &Searcher{Embedder: e, LLM: l, Store: s, TopK: 6}
}

// Answer runs a full RAG pipeline: embeds the question, retrieves top-k, streams generation.
// Tokens are delivered via onToken as they arrive; the final assembled answer is also returned.
// If onToken is nil, generation runs non-streaming.
func (s *Searcher) Answer(ctx context.Context, question string, onToken func(string)) (string, []store.Result, error) {
	filter, q := parseSourceFilter(question)
	if filter == "" {
		filter = s.SourceType
	}

	s.stage("embedding")
	vecs, err := s.Embedder.Embed(ctx, []string{q})
	if err != nil {
		return "", nil, err
	}

	s.stage("searching")
	results, err := s.Store.Search(ctx, vecs[0], s.TopK, filter)
	if err != nil {
		return "", nil, err
	}

	s.stage("generating")
	answer, err := s.LLM.Chat(ctx, []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: buildPrompt(q, results)},
	}, onToken)
	return answer, results, err
}

func (s *Searcher) stage(name string) {
	if s.OnStage != nil {
		s.OnStage(name)
	}
}

// parseSourceFilter strips an optional leading "source:<type>" token from the
// query and returns the filter plus the cleaned query. Unknown types pass through.
func parseSourceFilter(q string) (string, string) {
	q = strings.TrimSpace(q)
	if !strings.HasPrefix(q, "source:") {
		return "", q
	}
	rest := strings.TrimPrefix(q, "source:")
	sp := strings.IndexAny(rest, " \t")
	if sp < 0 {
		return "", q // just "source:foo" with no question; treat as plain query
	}
	t := strings.ToLower(rest[:sp])
	if t != "pdf" && t != "mbox" {
		return "", q
	}
	return t, strings.TrimSpace(rest[sp+1:])
}

// buildPrompt assembles a numbered context block and the user question.
func buildPrompt(question string, results []store.Result) string {
	var b strings.Builder
	b.WriteString("Context:\n\n")
	for i, r := range results {
		fmt.Fprintf(&b, "[%d] source=%s\n%s\n\n", i+1, r.Chunk.Source, r.Chunk.Content)
	}
	b.WriteString("Question: ")
	b.WriteString(question)
	return b.String()
}
