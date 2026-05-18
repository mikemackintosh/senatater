package extract

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"
)

// minTextChars is the threshold below which a PDF is considered "scanned"
// and worth running through OCR. Tuned for a typical page of prose; very
// short PDFs that legitimately have little text will incur an OCR pass.
const minTextChars = 100

// PDF extracts text from a PDF file. If the direct text layer is sparse
// (likely a scanned document), it transparently falls back to ocrmypdf.
// Requires `brew install poppler ocrmypdf` on macOS.
func PDF(ctx context.Context, path string) (string, error) {
	text, err := pdftotext(ctx, path)
	if err != nil {
		return "", err
	}
	if countVisible(text) >= minTextChars {
		return text, nil
	}
	ocrPath, cleanup, err := ocrmypdf(ctx, path)
	if err != nil {
		return text, nil
	}
	defer cleanup()
	ocrText, err := pdftotext(ctx, ocrPath)
	if err != nil {
		return text, nil
	}
	return ocrText, nil
}

// pdftotext invokes poppler's pdftotext in layout-preserving mode.
func pdftotext(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", path, "-")
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext %s: %w (%s)", path, err, errOut.String())
	}
	return out.String(), nil
}

// ocrmypdf produces a sidecar PDF with a fresh text layer. The caller must
// invoke the returned cleanup to remove the temp file.
func ocrmypdf(ctx context.Context, path string) (string, func(), error) {
	tmp, err := os.CreateTemp("", "emails-rag-ocr-*.pdf")
	if err != nil {
		return "", nil, err
	}
	tmp.Close()
	cleanup := func() { os.Remove(tmp.Name()) }

	cmd := exec.CommandContext(ctx, "ocrmypdf",
		"--skip-text",     // don't re-OCR pages that already have text
		"--quiet",
		"--optimize", "0", // skip optimization; we only need the text layer
		path, tmp.Name())
	var errOut strings.Builder
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("ocrmypdf %s: %w (%s)", path, err, errOut.String())
	}
	return tmp.Name(), cleanup, nil
}

// countVisible counts non-whitespace runes; a quick heuristic for whether
// a PDF has a usable text layer.
func countVisible(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}
