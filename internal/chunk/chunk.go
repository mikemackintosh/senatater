package chunk

import (
	"strings"
	"unicode"
)

// Options controls chunking behavior.
type Options struct {
	Size    int
	Overlap int
}

// Default returns a sensible default chunking config for RAG.
func Default() Options {
	return Options{Size: 1200, Overlap: 200}
}

// Split breaks text into overlapping chunks at paragraph boundaries where possible.
func Split(text string, opt Options) []string {
	if opt.Size <= 0 {
		opt.Size = 1200
	}
	text = normalize(text)
	if len(text) == 0 {
		return nil
	}

	runes := []rune(text)
	if len(runes) <= opt.Size {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return nil
		}
		return []string{trimmed}
	}

	var chunks []string
	start := 0
	for start < len(runes) {
		end := start + opt.Size
		if end >= len(runes) {
			c := strings.TrimSpace(string(runes[start:]))
			if c != "" {
				chunks = append(chunks, c)
			}
			break
		}
		cut := findBoundary(runes, start+opt.Size/2, end)
		c := strings.TrimSpace(string(runes[start:cut]))
		if c != "" {
			chunks = append(chunks, c)
		}
		start = cut - opt.Overlap
		if start < 0 {
			start = 0
		}
	}
	return chunks
}

// normalize collapses runs of whitespace while preserving paragraph breaks.
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var prevSpace, prevNewline bool
	for _, r := range s {
		if r == '\n' {
			if !prevNewline {
				b.WriteRune(r)
			}
			prevNewline = true
			prevSpace = true
			continue
		}
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
		prevNewline = false
	}
	return b.String()
}

// findBoundary returns the best split point between min and max.
// Prefers double newlines, then single newlines, then sentence ends, then whitespace.
func findBoundary(runes []rune, min, max int) int {
	if max > len(runes) {
		max = len(runes)
	}
	for i := max - 1; i >= min; i-- {
		if i+1 < len(runes) && runes[i] == '\n' && runes[i+1] == '\n' {
			return i + 2
		}
	}
	for i := max - 1; i >= min; i-- {
		if runes[i] == '\n' {
			return i + 1
		}
	}
	for i := max - 1; i >= min; i-- {
		if runes[i] == '.' || runes[i] == '!' || runes[i] == '?' {
			return i + 1
		}
	}
	for i := max - 1; i >= min; i-- {
		if unicode.IsSpace(runes[i]) {
			return i + 1
		}
	}
	return max
}
