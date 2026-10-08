// Package jobs runs long operations (backend installs, model downloads) as
// tracked background jobs and broadcasts progress over a single SSE feed so
// the UI can show live status without polling.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// JobStatus values.
const (
	StatusRunning = "running"
	StatusDone    = "done"
	StatusError   = "error"
)

// Job is one tracked background operation.
type Job struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"` // backend-install | model-install | ...
	Target   string    `json:"target"`
	Status   string    `json:"status"`
	Progress float64   `json:"progress"` // 0..1
	Message  string    `json:"message,omitempty"`
	Error    string    `json:"error,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

// Event is a broadcast update (one per job state change).
type Event struct {
	Type string `json:"type"` // progress | done | error
	Job  *Job   `json:"job"`
}

// Manager tracks jobs and fans out events to subscribers.
type Manager struct {
	mu   sync.RWMutex
	jobs map[string]*Job
	subs []chan Event
}

func NewManager() *Manager {
	return &Manager{jobs: map[string]*Job{}}
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Submit creates a job and runs fn in a goroutine. fn reports progress via
// the *Job (SetProgress / SetMessage); the manager broadcasts each change and
// finalizes the job when fn returns.
func (m *Manager) Submit(kind, target string, fn func(ctx context.Context, j *Job) error) *Job {
	j := &Job{
		ID: newID(), Kind: kind, Target: target,
		Status: StatusRunning, Created: time.Now(), Updated: time.Now(),
	}
	m.mu.Lock()
	m.jobs[j.ID] = j
	m.mu.Unlock()
	m.broadcast(Event{Type: "progress", Job: j})

	go func() {
		ctx := context.Background()
		err := fn(ctx, j)
		j.Updated = time.Now()
		if err != nil {
			j.Status = StatusError
			j.Error = err.Error()
			j.Progress = 0
			m.mu.Lock()
			m.jobs[j.ID] = j
			m.mu.Unlock()
			m.broadcast(Event{Type: "error", Job: j})
			return
		}
		j.Status = StatusDone
		j.Progress = 1
		m.mu.Lock()
		m.jobs[j.ID] = j
		m.mu.Unlock()
		m.broadcast(Event{Type: "done", Job: j})
	}()
	return j
}

// SetProgress updates a running job's progress and broadcasts.
func (j *Job) SetProgress(p float64, msg string) {
	j.Progress = p
	if msg != "" {
		j.Message = msg
	}
	j.Updated = time.Now()
}

// List returns a snapshot of all jobs, newest first.
func (m *Manager) List() []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		cp := *j
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Created.After(out[k].Created) })
	return out
}

// Get returns a copy of one job.
func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, false
	}
	cp := *j
	return &cp, true
}

// Subscribe returns a channel that receives all future events. The channel is
// buffered and never closed (callers range over it until done).
func (m *Manager) Subscribe() <-chan Event {
	ch := make(chan Event, 64)
	m.mu.Lock()
	m.subs = append(m.subs, ch)
	m.mu.Unlock()
	return ch
}

func (m *Manager) broadcast(ev Event) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.subs {
		select {
		case ch <- ev:
		default:
			// Slow subscriber: drop rather than block the broadcaster.
		}
	}
}
