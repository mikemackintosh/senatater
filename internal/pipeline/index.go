package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"emails-rag/internal/chunk"
	"emails-rag/internal/embed"
	"emails-rag/internal/extract"
	"emails-rag/internal/store"
)

// Indexer orchestrates extraction, chunking, embedding, and storage.
type Indexer struct {
	Embedder *embed.Client
	Store    *store.Store
	Log      *slog.Logger
	Chunker  chunk.Options
	Batch    int
}

// New returns an Indexer with sensible defaults.
func New(e *embed.Client, s *store.Store, log *slog.Logger) *Indexer {
	return &Indexer{
		Embedder: e,
		Store:    s,
		Log:      log,
		Chunker:  chunk.Default(),
		Batch:    16,
	}
}

// IndexPDF extracts, chunks, embeds, and stores a single PDF file.
func (i *Indexer) IndexPDF(ctx context.Context, path string) error {
	has, err := i.Store.HasSource(ctx, path)
	if err != nil {
		return err
	}
	if has {
		i.Log.Info("skipping already-indexed pdf", "path", path)
		return nil
	}

	text, err := extract.PDF(ctx, path)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	chunks := chunk.Split(text, i.Chunker)
	i.Log.Info("indexing pdf", "path", path, "chunks", len(chunks))

	return i.embedAndStore(ctx, path, "pdf", chunks, nil)
}

// IndexMBOX walks an MBOX file, chunking and storing each message.
func (i *Indexer) IndexMBOX(ctx context.Context, path string) error {
	has, err := i.Store.HasSource(ctx, path)
	if err != nil {
		return err
	}
	if has {
		i.Log.Info("skipping already-indexed mbox", "path", path)
		return nil
	}

	var count int
	err = extract.MBOX(path, func(m extract.Message) error {
		header := fmt.Sprintf("From: %s\nTo: %s\nDate: %s\nSubject: %s\n\n",
			m.From, m.To, m.Date, m.Subject)
		chunks := chunk.Split(header+m.Body, i.Chunker)
		meta := map[string]string{
			"from":    m.From,
			"to":      m.To,
			"subject": m.Subject,
			"date":    m.Date,
		}
		count += len(chunks)
		return i.embedAndStore(ctx, path, "mbox", chunks, meta)
	})
	i.Log.Info("indexed mbox", "path", path, "chunks", count)
	return err
}

// embedAndStore embeds chunks in batches and writes them to the store.
func (i *Indexer) embedAndStore(ctx context.Context, source, srcType string, chunks []string, meta map[string]string) error {
	for start := 0; start < len(chunks); start += i.Batch {
		end := start + i.Batch
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[start:end]
		vecs, err := i.Embedder.Embed(ctx, batch)
		if err != nil {
			return fmt.Errorf("embed: %w", err)
		}

		records := make([]store.Chunk, len(batch))
		for j, text := range batch {
			records[j] = store.Chunk{
				Source:     source,
				SourceType: srcType,
				Content:    text,
				Metadata:   meta,
				Embedding:  vecs[j],
			}
		}
		if err := i.Store.InsertBatch(ctx, records); err != nil {
			return fmt.Errorf("store: %w", err)
		}
	}
	return nil
}
