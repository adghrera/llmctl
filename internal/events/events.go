// Package events keeps a small in-memory ring of daemon events (instance
// crashes, auto-restarts, idle unloads) so the UI can show a recent-activity
// feed without a database (F099).
package events

import (
	"strings"
	"sync"
	"time"
)

// Event is one entry of the feed.
type Event struct {
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"` // e.g. "instance.crashed", "key.budget_exceeded"
	Instance string    `json:"instance,omitempty"`
	Model    string    `json:"model,omitempty"`
	Key      string    `json:"key,omitempty"` // key name for key.* events (F689)
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

// List returns events newest-first, at most limit (0 = all). When kind is
// non-empty only events of that kind are returned (exact match, F116).
func (r *Ring) List(limit int, kind, model string) []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, 0, len(r.evs))
	for i := len(r.evs) - 1; i >= 0; i-- {
		if kind != "" && r.evs[i].Kind != kind {
			continue
		}
		if model != "" && !strings.EqualFold(r.evs[i].Model, model) {
			continue
		}
		out = append(out, r.evs[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
