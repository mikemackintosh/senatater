package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"

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

	fmt.Println(`Ask away. Empty line exits.`)
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
		fmt.Println()
		ans, results, err := searcher.Answer(context.Background(), q, func(tok string) {
			fmt.Print(tok)
		})
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
			fmt.Printf("  [%d] %s (score=%.3f)\n", i+1, formatSource(r), r.Score)
		}
	}
}

// formatSource renders a hit's provenance. For email chunks, surfaces the
// subject and date from stored metadata so citations are useful for skim-back;
// for PDFs and anything else, falls back to the source path.
func formatSource(r store.Result) string {
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
