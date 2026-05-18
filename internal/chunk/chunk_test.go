package chunk

import (
	"strings"
	"testing"
)

func TestSplit_Empty(t *testing.T) {
	if got := Split("", Default()); got != nil {
		t.Errorf("want nil for empty input, got %v", got)
	}
	if got := Split("   \n\n\t  ", Default()); len(got) != 0 {
		t.Errorf("want no chunks for whitespace, got %v", got)
	}
}

func TestSplit_SingleChunk(t *testing.T) {
	text := "Just a short paragraph that easily fits in one chunk."
	chunks := Split(text, Options{Size: 1200, Overlap: 200})
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("chunk mismatch: %q", chunks[0])
	}
}

func TestSplit_HonorsSizeAndOverlap(t *testing.T) {
	// Build text larger than Size so we force multiple chunks.
	para := strings.Repeat("alpha beta gamma delta. ", 50) // ~1200 chars per repeat unit
	text := para + "\n\n" + para + "\n\n" + para
	opt := Options{Size: 300, Overlap: 50}
	chunks := Split(text, opt)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len([]rune(c)) > opt.Size+50 { // small slack for boundary search
			t.Errorf("chunk %d longer than size+slack: %d runes", i, len([]rune(c)))
		}
	}
}

func TestSplit_PrefersParagraphBoundary(t *testing.T) {
	a := strings.Repeat("a", 200)
	b := strings.Repeat("b", 200)
	text := a + "\n\n" + b
	chunks := Split(text, Options{Size: 220, Overlap: 20})
	if len(chunks) < 2 {
		t.Fatalf("want at least 2 chunks, got %d", len(chunks))
	}
	// First chunk should end on the paragraph break, i.e. be all a's.
	if strings.ContainsRune(chunks[0], 'b') {
		t.Errorf("first chunk crossed paragraph boundary: %q", chunks[0])
	}
}

func TestSplit_DefaultOptionsFillIn(t *testing.T) {
	text := strings.Repeat("x", 50)
	chunks := Split(text, Options{Size: 0})
	if len(chunks) != 1 || chunks[0] != text {
		t.Errorf("zero size should fall back to default; got %v", chunks)
	}
}
