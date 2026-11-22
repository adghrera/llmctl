// Package events keeps a small in-memory ring of daemon events (instance
// crashes, auto-restarts, idle unloads) so the UI can show a recent-activity
// feed without a database (F099).
package events

import (
	"sync"
	"time"
)

// Event is one entry of the feed.
type Event struct {
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"` // e.g. "instance.crashed"
	Instance string    `json:"instance,omitempty"`
	Model    string    `json:"model,omitempty"`
}

// Ring is a fixed-capacity event buffer, safe for concurrent use.
type Ring struct {
	mu  sync.Mutex
	evs []Event // oldest first
}

const ringMax = 200

// Add appends an event, dropping the oldest when full.
func (r *Ring) Add(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	r.evs = append(r.evs, e)
	if len(r.evs) > ringMax {
		r.evs = append([]Event{}, r.evs[len(r.evs)-ringMax:]...)
	}
}

// List returns events newest-first, at most limit (0 = all).
func (r *Ring) List(limit int) []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, 0, len(r.evs))
	for i := len(r.evs) - 1; i >= 0; i-- {
		out = append(out, r.evs[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
