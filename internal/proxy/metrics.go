package proxy

import (
	"bytes"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"llmctl/internal/cost"
)

// metricsCollector tracks gateway traffic: request counts, latency samples
// (reservoir-sampled), HTTP status classes, and tokens served (parsed from
// OpenAI "usage" fields, including usage chunks in SSE streams). Safe for
// concurrent use; all recording is O(1) on the request path.
type metricsCollector struct {
	mu sync.Mutex

	totalReqs  int64
	promptTok  int64
	compTok    int64
	latSamples []float64 // reservoir of latencies in ms
	seenReqs   int64

	byStatus map[string]int64    // "2xx", "4xx", ...
	byModel  map[string]*modelMt // per requested model

	keyCounters map[string]*costCounter // per API key prefix
	pricing     map[string]cost.ModelPricing
}

type modelMt struct {
	reqs      int64
	errs      int64
	promptTok int64
	compTok   int64
}

type costCounter struct {
	mu        sync.Mutex
	InputTok  int64
	OutputTok int64
	TotalTok  int64
	Requests  int64
	Cost      float64
	UpdatedAt time.Time
}

const reservoirSize = 2048

func newMetricsCollector() *metricsCollector {
	return &metricsCollector{
		byStatus:    map[string]int64{},
		byModel:     map[string]*modelMt{},
		keyCounters: map[string]*costCounter{},
		pricing:     make(map[string]cost.ModelPricing),
	}
}

func (m *metricsCollector) pricingFor(model string) cost.ModelPricing {
	if p, ok := m.pricing[model]; ok {
		return p
	}
	return cost.DefaultPricing["default"]
}

// record adds one completed gateway request. model may be "" (unknown).
func (m *metricsCollector) record(model, key string, status int, latencyMs float64, stream bool, respBody []byte) {
	var pTok, cTok int64
	if stream {
		pTok, cTok = scanUsageSSE(respBody)
	} else {
		pTok, cTok = scanUsageJSON(respBody)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalReqs++
	m.seenReqs++
	m.promptTok += pTok
	m.compTok += cTok
	cls := statusClass(status)
	m.byStatus[cls]++
	mm := m.byModel[model]
	if mm == nil {
		mm = &modelMt{}
		m.byModel[model] = mm
	}
	mm.reqs++
	if status >= 400 {
		mm.errs++
	}
	mm.promptTok += pTok
	mm.compTok += cTok

	// Per-key tracking.
	if key != "" {
		kc := m.keyCounters[key]
		if kc == nil {
			kc = &costCounter{}
			m.keyCounters[key] = kc
		}
		kc.mu.Lock()
		kc.InputTok += pTok
		kc.OutputTok += cTok
		kc.TotalTok += pTok + cTok
		kc.Requests++
		kc.Cost += float64(pTok)/1e6 * m.pricingFor(model).InputPerM
		kc.Cost += float64(cTok)/1e6 * m.pricingFor(model).OutputPerM
		kc.UpdatedAt = time.Now().UTC()
		kc.mu.Unlock()
	}

	// Reservoir sample latencies so memory stays bounded.
	if len(m.latSamples) < reservoirSize {
		m.latSamples = append(m.latSamples, latencyMs)
	} else {
		// Deterministic-ish replacement using the counter; good enough
		// for percentile estimates without importing math/rand locking.
		idx := int(m.seenReqs % int64(reservoirSize))
		m.latSamples[idx] = latencyMs
	}
}

func statusClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	case status >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}

// Snapshot renders the current metrics as JSON-ready maps.
func (m *metricsCollector) Snapshot() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	lat := map[string]any{}
	if len(m.latSamples) > 0 {
		s := append([]float64(nil), m.latSamples...)
		sort.Float64s(s)
		lat["p50_ms"] = round2(percentile(s, 0.50))
		lat["p90_ms"] = round2(percentile(s, 0.90))
		lat["p99_ms"] = round2(percentile(s, 0.99))
		lat["samples"] = len(s)
	}

	models := map[string]any{}
	for k, v := range m.byModel {
		name := k
		if name == "" {
			name = "(unknown)"
		}
		models[name] = map[string]any{
			"requests":          v.reqs,
			"errors":            v.errs,
			"prompt_tokens":     v.promptTok,
			"completion_tokens": v.compTok,
		}
	}

	status := map[string]any{}
	for k, v := range m.byStatus {
		status[k] = v
	}

	return map[string]any{
		"requests_total": m.totalReqs,
		"tokens": map[string]any{
			"prompt":     m.promptTok,
			"completion": m.compTok,
			"total":      m.promptTok + m.compTok,
		},
		"latency":        lat,
		"status_classes": status,
		"models":         models,
	}
}

func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * q)
	return sorted[idx]
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// ---- usage parsing ----

type usageEnvelope struct {
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func scanUsageJSON(b []byte) (prompt, completion int64) {
	if len(b) == 0 || len(b) > 4<<20 {
		return 0, 0
	}
	var u usageEnvelope
	if err := json.Unmarshal(b, &u); err != nil || u.Usage == nil {
		return 0, 0
	}
	return int64(u.Usage.PromptTokens), int64(u.Usage.CompletionTokens)
}

// scanUsageSSE walks "data: {...}" lines of a buffered SSE body and returns
// the last usage object seen (OpenAI streams usage in a final chunk).
func scanUsageSSE(b []byte) (prompt, completion int64) {
	if len(b) == 0 || len(b) > 4<<20 {
		return 0, 0
	}
	for _, line := range bytes.Split(b, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		var u usageEnvelope
		if err := json.Unmarshal(payload, &u); err == nil && u.Usage != nil {
			prompt, completion = int64(u.Usage.PromptTokens), int64(u.Usage.CompletionTokens)
		}
	}
	return prompt, completion
}
