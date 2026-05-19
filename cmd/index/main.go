package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"emails-rag/internal/embed"
	"emails-rag/internal/pipeline"
	"emails-rag/internal/store"
)

func main() {
	var (
		dbPath     = flag.String("db", "data/index.db", "path to the sqlite index file")
		embedModel = flag.String("embed-model", "nomic-embed-text", "ollama embedding model name")
		force      = flag.Bool("force", false, "re-ingest sources already in the index (deletes prior chunks for each touched source)")
		pdfDirs    multiFlag
		mboxFiles  multiFlag
	)
	flag.Var(&pdfDirs, "pdf-dir", "directory to walk for .pdf files (repeatable)")
	flag.Var(&mboxFiles, "mbox", "path to an mbox file to index (repeatable)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Error("create db dir", "err", err)
		os.Exit(1)
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer s.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	e := embed.New(*embedModel)
	idx := pipeline.New(e, s, log)
	idx.Force = *force

	for _, dir := range pdfDirs {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if !strings.EqualFold(filepath.Ext(path), ".pdf") {
				return nil
			}
			if err := idx.IndexPDF(ctx, path); err != nil {
				log.Error("index pdf", "path", path, "err", err)
			}
			return nil
		})
		if err != nil {
			log.Error("walk pdfs", "dir", dir, "err", err)
		}
	}

	for _, mbox := range mboxFiles {
		if err := idx.IndexMBOX(ctx, mbox); err != nil {
			log.Error("index mbox", "path", mbox, "err", err)
		}
	}

	total, _ := s.Stats(ctx)
	log.Info("indexing complete", "total_chunks", total)
}

// multiFlag collects repeated string flag values into a slice.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
