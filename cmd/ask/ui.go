package main

import (
	"fmt"
	"os"
	"strings"

	"emails-rag/internal/store"
)

// ANSI escape sequences. Kept inline rather than pulling a color library
// because the surface area is tiny and we want zero dependencies.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiItalic = "\033[3m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

// ui handles all terminal styling for the ask REPL. Construct via newUI
// so color detection happens once.
type ui struct {
	color bool
}

func newUI(forceNoColor bool) *ui {
	color := !forceNoColor && os.Getenv("NO_COLOR") == "" && isStdoutTerminal()
	return &ui{color: color}
}

// isStdoutTerminal reports whether stdout is attached to a TTY. We use
// the character-device bit rather than pulling golang.org/x/term — it's
// good enough for our pipe/redirect-detection needs.
func isStdoutTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (u *ui) wrap(code, s string) string {
	if !u.color {
		return s
	}
	return code + s + ansiReset
}

func (u *ui) bold(s string) string   { return u.wrap(ansiBold, s) }
func (u *ui) dim(s string) string    { return u.wrap(ansiDim, s) }
func (u *ui) italic(s string) string { return u.wrap(ansiItalic, s) }
func (u *ui) red(s string) string    { return u.wrap(ansiRed, s) }
func (u *ui) yellow(s string) string { return u.wrap(ansiYellow, s) }
func (u *ui) cyan(s string) string   { return u.wrap(ansiCyan, s) }

// bannerConfig is the subset of runtime config worth showing on startup.
type bannerConfig struct {
	DBPath     string
	ChunkCount int
	EmbedModel string
	EmbedURL   string
	ChatModel  string
	ChatURL    string
	ChatAPI    string
	TopK       int
	Source     string
}

// printBanner writes the startup banner showing all active settings.
// Tells the user exactly which backends and model are wired up so a wrong
// flag combination is visible before the first query.
func (u *ui) printBanner(cfg bannerConfig) {
	fmt.Println(u.bold("emails-rag"))

	line := func(label, value, suffix string) {
		fmt.Printf("  %-7s %s%s\n", u.dim(label+":"), value, suffix)
	}

	chunks := u.dim(fmt.Sprintf("  (%d chunks)", cfg.ChunkCount))
	if cfg.ChunkCount == 0 {
		chunks = u.yellow("  (empty — run ./cmd/index first)")
	}
	line("index", cfg.DBPath, chunks)
	line("embed", cfg.EmbedModel, u.dim("  via "+cfg.EmbedURL))
	line("chat", cfg.ChatModel, u.dim(fmt.Sprintf("  via %s  (%s)", cfg.ChatURL, cfg.ChatAPI)))
	line("top-k", fmt.Sprintf("%d", cfg.TopK), "")
	if cfg.Source != "" {
		line("source", cfg.Source, u.dim("  (filtering)"))
	}

	fmt.Println()
	fmt.Println(u.dim("Empty line exits. Ctrl-C cancels the current query."))
	fmt.Println(u.dim(`Prefix with "source:pdf" or "source:mbox" to filter a single query.`))
}

// prompt writes the input prompt. Always flushed via Print so the cursor
// lands right after.
func (u *ui) prompt() {
	fmt.Print("\n" + u.cyan(u.bold(">")) + " ")
}

// showStage writes a transient stage indicator to stderr. Subsequent
// indicators overwrite via carriage return; clearStage erases.
func (u *ui) showStage(name string) {
	fmt.Fprintf(os.Stderr, "\r%s%s", u.dim("("+name+"...)"), strings.Repeat(" ", 16))
}

func (u *ui) clearStage() {
	fmt.Fprint(os.Stderr, "\r"+strings.Repeat(" ", 32)+"\r")
}

// renderSources prints the divider + Sources block under a completed answer.
func (u *ui) renderSources(results []store.Result) {
	if len(results) == 0 {
		return
	}
	fmt.Println()
	fmt.Println(u.dim(strings.Repeat("─", 60)))
	fmt.Println(u.bold(u.yellow("Sources")))
	for i, r := range results {
		fmt.Printf("  %s %s %s\n",
			u.cyan(fmt.Sprintf("[%d]", i+1)),
			u.dim(fmt.Sprintf("(%.3f)", r.Score)),
			sourceHeader(r))
		fmt.Printf("      %s\n", u.dim(u.italic(snippet(r.Chunk.Content, 100))))
	}
}

// errorf writes a colored error line to stderr.
func (u *ui) errorf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, u.red("error:")+" "+fmt.Sprintf(format, args...))
}

// notice writes a colored notice line (cancelled, no response, ...) to stderr.
func (u *ui) notice(msg string) {
	fmt.Fprintln(os.Stderr, u.yellow(msg))
}
