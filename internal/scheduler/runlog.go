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

const runLogSize = 200

var (
	runsMu  sync.Mutex
	runsBuf []Run
)

// LogRun appends a completed run to the ring (trimmed to the newest 200).
func LogRun(r Run) {
	runsMu.Lock()
	defer runsMu.Unlock()
	runsBuf = append(runsBuf, r)
	if len(runsBuf) > runLogSize {
		runsBuf = append(runsBuf[:0], runsBuf[len(runsBuf)-runLogSize:]...)
	}
}

// Runs returns up to n recent runs, newest first.
func Runs(n int) []Run { return RunsFiltered(n, "") }

// RunsFiltered is Runs plus an optional task filter: when task is non-empty
// only runs whose task id or name match exactly are kept (F699).
func RunsFiltered(n int, task string) []Run {
	runsMu.Lock()
	defer runsMu.Unlock()
	out := make([]Run, 0, n)
	for i := len(runsBuf) - 1; i >= 0; i-- {
		r := runsBuf[i]
		if task != "" && r.TaskID != task && r.Name != task {
			continue
		}
		out = append(out, r)
		if len(out) >= n {
			break
		}
	}
	return out
}

// LastRun returns the most recent run for a task id, or nil if the ring has no
// entry for it (the ring is in-memory, so this is nil after a daemon restart
// until the task runs again) (F184).
func LastRun(taskID string) *Run {
	runsMu.Lock()
	defer runsMu.Unlock()
	for i := len(runsBuf) - 1; i >= 0; i-- {
		if runsBuf[i].TaskID == taskID {
			r := runsBuf[i]
			return &r
		}
	}
	return nil
}
