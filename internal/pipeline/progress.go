package pipeline

import (
	"log/slog"
	"sync"
	"time"
)

// progressInterval is the minimum gap between progress log emissions.
// A tick that arrives sooner than this is silently dropped; this keeps
// the log readable on a fast indexer without losing visibility on a slow
// one. Tuned by hand: ~every 2 s feels alive without being noisy.
const progressInterval = 2 * time.Second

// progress is a time-throttled status emitter for long-running loops.
// Callers invoke tick() per unit of work; only ticks far enough apart
// in wall time actually produce log output. Safe for concurrent use.
type progress struct {
	log   *slog.Logger
	label string

	mu       sync.Mutex
	lastEmit time.Time
}

func newProgress(log *slog.Logger, label string) *progress {
	return &progress{
		log:      log,
		label:    label,
		lastEmit: time.Now(),
	}
}

// tick emits a log line at the configured label and keys, but only if
// enough time has elapsed since the previous emit. The caller is
// responsible for passing the current counters as structured fields.
func (p *progress) tick(keys ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if now.Sub(p.lastEmit) < progressInterval {
		return
	}
	p.lastEmit = now
	p.log.Info(p.label, keys...)
}
