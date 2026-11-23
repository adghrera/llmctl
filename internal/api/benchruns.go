package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// BenchRun is one completed benchmark run kept in a small in-memory ring so
// the Bench page and `llmctl bench history` can show recent results
// (F846). The CLI and the UI both POST a summary after a run finishes.
type BenchRun struct {
	Time      time.Time `json:"time"`
	Model     string    `json:"model"`
	N         int       `json:"n"`
	Conc      int       `json:"conc"`
	Prompt    string    `json:"prompt,omitempty"`
	OK        int       `json:"ok"`
	Failed    int       `json:"failed"`
	AvgMs     float64   `json:"avgMs"`
	P50Ms     float64   `json:"p50Ms"`
	P95Ms     float64   `json:"p95Ms"`
	TokPerSec float64   `json:"tokPerSec"`
	ReqPerSec float64   `json:"reqPerSec"`
	Source    string    `json:"source,omitempty"` // "cli" | "ui"
}

const benchRunLogSize = 100

var (
	benchRunsMu  sync.Mutex
	benchRunsBuf []BenchRun
)

// logBenchRun appends a run to the ring (trimmed to the newest 100).
func logBenchRun(r BenchRun) {
	benchRunsMu.Lock()
	defer benchRunsMu.Unlock()
	benchRunsBuf = append(benchRunsBuf, r)
	if len(benchRunsBuf) > benchRunLogSize {
		benchRunsBuf = append(benchRunsBuf[:0], benchRunsBuf[len(benchRunsBuf)-benchRunLogSize:]...)
	}
}

// benchRunList returns up to n recent runs, newest first.
func benchRunList(n int) []BenchRun {
	benchRunsMu.Lock()
	defer benchRunsMu.Unlock()
	out := make([]BenchRun, 0, n)
	for i := len(benchRunsBuf) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, benchRunsBuf[i])
	}
	return out
}

// logBenchRunHandler handles POST /api/v1/bench/runs — record one finished
// benchmark run (F846). The server stamps the time.
func (s *Server) logBenchRunHandler(w http.ResponseWriter, r *http.Request) {
	var run BenchRun
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&run); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if run.Model == "" || run.N <= 0 {
		writeErr(w, 400, "model and n are required")
		return
	}
	if len(run.Prompt) > 120 {
		run.Prompt = run.Prompt[:120]
	}
	run.Time = time.Now().UTC()
	logBenchRun(run)
	writeJSON(w, 201, run)
}

// listBenchRuns handles GET /api/v1/bench/runs?n=20 — recent bench runs
// (F846).
func (s *Server) listBenchRuns(w http.ResponseWriter, r *http.Request) {
	n := 20
	if v := r.URL.Query().Get("n"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			n = p
		}
	}
	if n > benchRunLogSize {
		n = benchRunLogSize
	}
	writeJSON(w, 200, benchRunList(n))
}
