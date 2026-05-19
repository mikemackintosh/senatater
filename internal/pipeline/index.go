package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// PDFs dedup by file path: indexing the same path twice is a no-op.
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

	return i.embedAndStore(ctx, path, "pdf", "", chunks, nil)
}

// IndexMBOX walks an MBOX file, indexing each new message. Dedup is per
// message rather than per file, so appending mail to an existing archive
// only ingests the new messages on the next run.
func (i *Indexer) IndexMBOX(ctx context.Context, path string) error {
	var indexed, skipped, chunkCount int
	err := extract.MBOX(path, func(m extract.Message) error {
		id := MessageDedupID(m)
		has, err := i.Store.HasMessageID(ctx, id)
		if err != nil {
			return err
		}
		if has {
			skipped++
			return nil
		}

		header := fmt.Sprintf("From: %s\nTo: %s\nDate: %s\nSubject: %s\n\n",
			m.From, m.To, m.Date, m.Subject)
		chunks := chunk.Split(header+m.Body, i.Chunker)
		if len(chunks) == 0 {
			return nil
		}
		meta := map[string]string{
			"from":    m.From,
			"to":      m.To,
			"subject": m.Subject,
			"date":    m.Date,
		}
		if err := i.embedAndStore(ctx, path, "mbox", id, chunks, meta); err != nil {
			return err
		}
		// Counters only advance once the chunks are actually persisted, so
		// the summary log line never overstates progress on partial failures.
		chunkCount += len(chunks)
		indexed++
		return nil
	})
	i.Log.Info("indexed mbox",
		"path", path,
		"messages_indexed", indexed,
		"messages_skipped", skipped,
		"chunks", chunkCount,
	)
	return err
}

// MessageDedupID returns the stable identifier used for per-message dedup.
// Prefers the RFC 5322 Message-ID header; falls back to a content
// fingerprint for messages missing one (the "fp:" prefix distinguishes
// fingerprints from real Message-IDs, which are wrapped in angle brackets).
func MessageDedupID(m extract.Message) string {
	if m.MessageID != "" {
		return m.MessageID
	}
	body := m.Body
	if len(body) > 256 {
		body = body[:256]
	}
	h := sha256.Sum256([]byte(m.From + "|" + m.Date + "|" + m.Subject + "|" + body))
	return "fp:" + hex.EncodeToString(h[:8])
}

// embedAndStore embeds chunks in batches and writes them to the store.
func (i *Indexer) embedAndStore(ctx context.Context, source, srcType, messageID string, chunks []string, meta map[string]string) error {
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
				MessageID:  messageID,
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
