package proxy

import (
	"sort"
	"sync"
	"time"
)

// RequestLogEntry is one entry of the recent-requests ring (F061).
type RequestLogEntry struct {
	Time      time.Time `json:"time"`
	Model     string    `json:"model,omitempty"`
	Key       string    `json:"key,omitempty"` // API key prefix (first 8 chars)
	Status    int       `json:"status"`
	LatencyMs float64   `json:"latencyMs"`
	PromptTok int64     `json:"promptTok"`
	CompTok   int64     `json:"compTok"`
	Stream    bool      `json:"stream,omitempty"`
}

const requestLogSize = 256

// requestLog is a fixed-size ring of the most recent gateway requests.
// Safe for concurrent use.
type requestLog struct {
	mu    sync.Mutex
	buf   []RequestLogEntry // filled up to requestLogSize, then overwritten round-robin
	head  int               // next write slot once full
	count int
}

func (l *requestLog) add(e RequestLogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) < requestLogSize {
		l.buf = append(l.buf, e)
		l.count++
		return
	}
	l.buf[l.head] = e
	l.head = (l.head + 1) % requestLogSize
}

// recent returns up to n newest-first entries (n <= 0 = all).
func (l *requestLog) recent(n int) []RequestLogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]RequestLogEntry, len(l.buf))
	copy(out, l.buf)
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
