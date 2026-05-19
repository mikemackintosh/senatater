package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
		chatModel  = flag.String("chat-model", "qwen3:30b-a3b", "chat model name (Ollama tag or HF id depending on -chat-api)")
		chatAPI    = flag.String("chat-api", "ollama", `chat backend: "ollama" or "openai" (OpenAI-compatible, also vLLM / LM Studio)`)
		chatURL    = flag.String("chat-url", "", "chat server base URL; default localhost:11434 for ollama, localhost:8000 for openai")
		chatKey    = flag.String("chat-key", "", "bearer token for OpenAI-compatible servers (optional)")
		topK       = flag.Int("k", 6, "number of chunks to retrieve")
		source     = flag.String("source", "", `default source filter: "pdf", "mbox", or "" for all`)
		noColor    = flag.Bool("no-color", false, "disable ANSI colors (NO_COLOR env var also honored)")
	)
	flag.Parse()

	u := newUI(*noColor)

	if *source != "" && *source != "pdf" && *source != "mbox" {
		u.errorf(`-source must be "pdf", "mbox", or empty`)
		os.Exit(2)
	}
	if *chatAPI != "ollama" && *chatAPI != "openai" {
		u.errorf(`-chat-api must be "ollama" or "openai"`)
		os.Exit(2)
	}

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		u.errorf("create db dir: %v", err)
		os.Exit(1)
	}
	s, err := store.Open(*dbPath)
	if err != nil {
		u.errorf("open store: %v", err)
		os.Exit(1)
	}
	defer s.Close()

	chunkCount, _ := s.Stats(context.Background())

	e := embed.New(*embedModel)
	l := llm.New(llm.Backend(*chatAPI), *chatModel, *chatURL, *chatKey)
	searcher := pipeline.NewSearcher(e, l, s)
	searcher.TopK = *topK
	searcher.SourceType = *source

	u.printBanner(bannerConfig{
		DBPath:     *dbPath,
		ChunkCount: chunkCount,
		EmbedModel: *embedModel,
		EmbedURL:   "http://localhost:11434",
		ChatModel:  *chatModel,
		ChatURL:    chatURLDisplay(*chatAPI, *chatURL),
		ChatAPI:    *chatAPI,
		TopK:       *topK,
		Source:     *source,
	})

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
				u.notice("\n(interrupted)")
				os.Exit(130)
			}
		}
	}()

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 4096), 1<<20)

	for {
		u.prompt()
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
		var stagePrinted bool
		searcher.OnStage = func(stage string) {
			u.showStage(stage)
			stagePrinted = true
		}
		ans, results, err := searcher.Answer(ctx, q, func(tok string) {
			if stagePrinted {
				u.clearStage()
				stagePrinted = false
			}
			fmt.Print(tok)
		})

		cancelMu.Lock()
		cancelCurrent = nil
		cancelMu.Unlock()
		cancel()

		if stagePrinted {
			u.clearStage()
		}

		if errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled {
			u.notice("\n(cancelled)")
			continue
		}
		if err != nil {
			u.errorf("%v", err)
			continue
		}
		if ans == "" {
			u.notice("(no response)")
		} else {
			fmt.Println()
		}
		u.renderSources(results)
	}
}

// chatURLDisplay returns the chat server URL the client will actually use,
// resolving the empty-default-per-backend rule so the banner doesn't show
// a misleading blank.
func chatURLDisplay(api, url string) string {
	if url != "" {
		return url
	}
	if api == "openai" {
		return "http://localhost:8000"
	}
	return "http://localhost:11434"
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
