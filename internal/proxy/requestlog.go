package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// RequestLogEntry is one entry of the recent-requests ring (F061).
type RequestLogEntry struct {
	ID        string    `json:"id,omitempty"` // request id, echoed as X-Request-Id (F092)
	Time      time.Time `json:"time"`
	Model     string    `json:"model,omitempty"`
	Key       string    `json:"key,omitempty"`      // API key prefix (first 8 chars)
	KeyID     string    `json:"keyId,omitempty"`    // API key id for exact filtering (F120)
	Instance  string    `json:"instance,omitempty"` // replica that served it (F115)
	Status    int       `json:"status"`
	LatencyMs float64   `json:"latencyMs"`
	TtftMs    float64   `json:"ttftMs,omitempty"` // streaming time-to-first-token (F112)
	PromptTok int64     `json:"promptTok"`
	CompTok   int64     `json:"compTok"`
	Stream    bool      `json:"stream,omitempty"`
}

const requestLogSize = 256

// persistedLogKeep is how many lines to keep in the JSONL file on startup
// trim (F086).
const persistedLogKeep = 1000

// requestLog is a fixed-size ring of the most recent gateway requests.
// When a file is attached (F086) every entry is also appended as one JSON
// line so the log survives daemon restarts. Safe for concurrent use.
type requestLog struct {
	mu    sync.Mutex
	buf   []RequestLogEntry // filled up to requestLogSize, then overwritten round-robin
	head  int               // next write slot once full
	count int
	file  *os.File // JSONL append target; nil = disabled
}

// useFile enables JSONL persistence at path: the file is trimmed to the last
// persistedLogKeep lines and the ring is seeded from its newest entries.
// Best-effort: on any error persistence stays disabled.
func (l *requestLog) useFile(path string) {
	lines := []string{}
	if raw, err := os.ReadFile(path); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(raw))
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			if s := sc.Text(); s != "" {
				lines = append(lines, s)
			}
		}
		if len(lines) > persistedLogKeep {
			lines = lines[len(lines)-persistedLogKeep:]
		}
		// Seed the ring oldest-first so the newest entries end up kept.
		l.mu.Lock()
		for _, s := range lines {
			var e RequestLogEntry
			if json.Unmarshal([]byte(s), &e) == nil {
				l.addLocked(e)
			}
		}
		l.mu.Unlock()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	l.mu.Lock()
	l.file = f
	l.mu.Unlock()
	// Rewrite the trimmed file so it cannot grow unbounded.
	if len(lines) > 0 {
		trimmed := ""
		for _, s := range lines {
			trimmed += s + "\n"
		}
		if os.WriteFile(path, []byte(trimmed), 0o644) == nil {
			f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err == nil {
				l.mu.Lock()
				l.file = f
				l.mu.Unlock()
			}
		}
	}
}

func (l *requestLog) add(e RequestLogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.addLocked(e)
	if l.file != nil {
		if b, err := json.Marshal(e); err == nil {
			l.file.Write(append(b, '\n'))
		}
	}
}

func (l *requestLog) addLocked(e RequestLogEntry) {
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

// byID returns the ring entry with the given request id (F104).
func (l *requestLog) byID(id string) (RequestLogEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.buf {
		if e.ID == id {
			return e, true
		}
	}
	return RequestLogEntry{}, false
}
