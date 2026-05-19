package pipeline

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestProgress_ThrottlesEmits(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	p := newProgress(log, "test")

	// First tick is suppressed because newProgress sets lastEmit to "now".
	// A flurry of immediate ticks must collapse to at most one emission
	// after the throttle interval elapses, and zero before.
	for i := 0; i < 50; i++ {
		p.tick("count", i)
	}
	if got := strings.Count(buf.String(), "msg=test"); got != 0 {
		t.Errorf("expected 0 emits before interval, got %d", got)
	}

	// Force the interval to have passed.
	p.mu.Lock()
	p.lastEmit = time.Now().Add(-progressInterval - time.Second)
	p.mu.Unlock()

	p.tick("count", 100)
	if got := strings.Count(buf.String(), "msg=test"); got != 1 {
		t.Errorf("expected 1 emit after interval, got %d", got)
	}

	// Subsequent ticks within the next interval are again suppressed.
	for i := 0; i < 10; i++ {
		p.tick("count", i)
	}
	if got := strings.Count(buf.String(), "msg=test"); got != 1 {
		t.Errorf("expected still 1 emit, got %d", got)
	}
}
