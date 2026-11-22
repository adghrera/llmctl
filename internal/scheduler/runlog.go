package scheduler

import (
	"sync"
	"time"
)

// Run is one task execution kept in a small in-memory ring so the UI can show
// what ran recently and whether it succeeded (F145).
type Run struct {
	Time       time.Time `json:"time"`
	TaskID     string    `json:"taskId"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"` // "ok" | "error"
	DurationMs int64     `json:"durationMs"`
	Detail     string    `json:"detail,omitempty"` // error text or result excerpt
}

const runLogSize = 50

var (
	runsMu  sync.Mutex
	runsBuf []Run
)

// LogRun appends a completed run to the ring (trimmed to the newest 50).
func LogRun(r Run) {
	runsMu.Lock()
	defer runsMu.Unlock()
	runsBuf = append(runsBuf, r)
	if len(runsBuf) > runLogSize {
		runsBuf = append(runsBuf[:0], runsBuf[len(runsBuf)-runLogSize:]...)
	}
}

// Runs returns up to n recent runs, newest first.
func Runs(n int) []Run {
	runsMu.Lock()
	defer runsMu.Unlock()
	out := make([]Run, 0, n)
	for i := len(runsBuf) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, runsBuf[i])
	}
	return out
}
