package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"emails-rag/internal/embed"
	"emails-rag/internal/llm"
	"emails-rag/internal/pipeline"
	"emails-rag/internal/store"
)

func main() {
	var (
		dbPath     = flag.String("db", "data/index.db", "path to the sqlite index file")
		embedModel = flag.String("embed-model", "nomic-embed-text", "ollama embedding model name")
		chatModel  = flag.String("chat-model", "qwen3:30b-a3b", "ollama chat model name")
		topK       = flag.Int("k", 6, "number of chunks to retrieve")
		source     = flag.String("source", "", `default source filter: "pdf", "mbox", or "" for all`)
	)
	flag.Parse()

	if *source != "" && *source != "pdf" && *source != "mbox" {
		fmt.Fprintln(os.Stderr, `-source must be "pdf", "mbox", or empty`)
		os.Exit(2)
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		os.Exit(1)
	}
	defer s.Close()

	e := embed.New(*embedModel)
	l := llm.New(*chatModel)
	searcher := pipeline.NewSearcher(e, l, s)
	searcher.TopK = *topK
	searcher.SourceType = *source

	// Ctrl-C during a query cancels that query; Ctrl-C at the prompt exits.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	var (
		cancelMu      sync.Mutex
		cancelCurrent context.CancelFunc
	)
	go func() {
		for range sigCh {
			cancelMu.Lock()
			c := cancelCurrent
			cancelCurrent = nil
			cancelMu.Unlock()
			if c != nil {
				c()
			} else {
				fmt.Fprintln(os.Stderr, "\n(interrupted)")
				os.Exit(130)
			}
		}
	}()

	fmt.Println(`Ask away. Empty line exits. Ctrl-C cancels the current query.`)
	fmt.Println(`Prefix with "source:pdf " or "source:mbox " to filter a single query.`)
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 4096), 1<<20)

	for {
		fmt.Print("\n> ")
		if !in.Scan() {
			break
		}
		q := in.Text()
		if q == "" {
			break
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancelMu.Lock()
		cancelCurrent = cancel
		cancelMu.Unlock()

		fmt.Println()
		ans, results, err := searcher.Answer(ctx, q, func(tok string) {
			fmt.Print(tok)
		})

		cancelMu.Lock()
		cancelCurrent = nil
		cancelMu.Unlock()
		cancel()

		if errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled {
			fmt.Fprintln(os.Stderr, "\n(cancelled)")
			continue
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "\nerror:", err)
			continue
		}
		if ans == "" {
			fmt.Println("(no response)")
		} else {
			fmt.Println()
		}
		fmt.Println("\nSources:")
		for i, r := range results {
			fmt.Printf("  [%d] %s (score=%.3f)\n", i+1, sourceHeader(r), r.Score)
			fmt.Printf("      %s\n", snippet(r.Chunk.Content, 100))
		}
	}
}

// sourceHeader renders a hit's provenance. For email chunks, surfaces the
// subject and date from stored metadata so citations are useful for skim-back;
// for PDFs and anything else, falls back to the source path.
func sourceHeader(r store.Result) string {
	if r.Chunk.SourceType != "mbox" {
		return r.Chunk.Source
	}
	subject := r.Chunk.Metadata["subject"]
	date := r.Chunk.Metadata["date"]
	switch {
	case subject != "" && date != "":
		return fmt.Sprintf(`%s: %q — %s`, r.Chunk.Source, subject, date)
	case subject != "":
		return fmt.Sprintf(`%s: %q`, r.Chunk.Source, subject)
	case date != "":
		return fmt.Sprintf("%s — %s", r.Chunk.Source, date)
	default:
		return r.Chunk.Source
	}
}

// snippet collapses whitespace and truncates to n runes for a one-line preview.
func snippet(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
