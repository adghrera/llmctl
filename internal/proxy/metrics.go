package proxy

import (
	"bytes"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"llmctl/internal/cost"
	"llmctl/internal/store"
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

	lastUsed map[string]time.Time // last gateway request completion per model
	reqLog   *requestLog          // recent-requests ring (F061)

	keyCounters map[string]*costCounter // per API key prefix
	pricing     map[string]cost.ModelPricing
	daily       map[string]map[string]*store.DailyModelUsage // UTC date -> model (F097)
}

type modelMt struct {
	reqs       int64
	errs       int64
	promptTok  int64
	compTok    int64
	latSamples []float64 // per-model reservoir of latencies in ms
	seen       int64
	// Per-request completion-token throughput (F110): sum of cTok/latency
	// over served requests, and their count.
	tpsSum float64
	tpsN   int64
	// Per-request time-to-first-token in ms, streaming served requests (F112).
	ttftSum float64
	ttftN   int64
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
		lastUsed:    map[string]time.Time{},
		reqLog:      &requestLog{},
		keyCounters: map[string]*costCounter{},
		pricing:     make(map[string]cost.ModelPricing),
		daily:       map[string]map[string]*store.DailyModelUsage{},
	}
}

// setPricing replaces the per-model pricing table used for cost tracking.
func (m *metricsCollector) setPricing(p map[string]cost.ModelPricing) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pricing = p
}

// setModelPricing updates one model's pricing entry (F102).
func (m *metricsCollector) setModelPricing(model string, p cost.ModelPricing) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pricing[model] = p
}

func (m *metricsCollector) pricingFor(model string) cost.ModelPricing {
	if p, ok := m.pricing[model]; ok {
		return p
	}
	return cost.DefaultPricing["default"]
}

// record adds one completed gateway request. model may be "" (unknown).
func (m *metricsCollector) record(model, key, keyLabel, instance string, status int, latencyMs, ttftMs float64, stream bool, respBody []byte, rid string) {
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
	if model != "" {
		m.lastUsed[model] = time.Now()
	}
	mm.reqs++
	if status >= 400 {
		mm.errs++
	}
	mm.promptTok += pTok
	mm.compTok += cTok
	if status < 400 && cTok > 0 && latencyMs > 0 {
		mm.tpsSum += float64(cTok) / (latencyMs / 1000.0)
		mm.tpsN++
	}
	if status < 400 && ttftMs > 0 {
		mm.ttftSum += ttftMs
		mm.ttftN++
	}
	// Daily rollup (F097): UTC date -> model -> counters.
	day := time.Now().UTC().Format("2006-01-02")
	dm := m.daily[day]
	if dm == nil {
		dm = map[string]*store.DailyModelUsage{}
		m.daily[day] = dm
	}
	du := dm[model]
	if du == nil {
		du = &store.DailyModelUsage{}
		dm[model] = du
	}
	du.Requests++
	if status >= 400 {
		du.Errors++
	}
	du.Prompt += pTok
	du.Comp += cTok
	du.Cost += float64(pTok)/1e6*m.pricingFor(model).InputPerM + float64(cTok)/1e6*m.pricingFor(model).OutputPerM
	// Per-model latency reservoir (bounded, same policy as global).
	mm.seen++
	if len(mm.latSamples) < reservoirSize {
		mm.latSamples = append(mm.latSamples, latencyMs)
	} else {
		mm.latSamples[int(mm.seen%int64(reservoirSize))] = latencyMs
	}

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
		if status < 400 {
			kc.Requests++ // only served requests count toward per-key quotas (F105)
		}
		kc.Cost += float64(pTok)/1e6 * m.pricingFor(model).InputPerM
		kc.Cost += float64(cTok)/1e6 * m.pricingFor(model).OutputPerM
		kc.UpdatedAt = time.Now().UTC()
		kc.mu.Unlock()
	}

	// Recent-requests ring (F061). Show the readable prefix, not the id.
	if keyLabel == "" {
		keyLabel = key
	}
	m.reqLog.add(RequestLogEntry{
		ID:        rid,
		Time:      time.Now(),
		Model:     model,
		Key:       keyLabel,
		KeyID:     key,
		Instance:  instance,
		Status:    status,
		LatencyMs: latencyMs,
		TtftMs:    ttftMs,
		PromptTok: pTok,
		CompTok:   cTok,
		Stream:    stream,
	})

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

// lastUsedAt returns the last recorded request time for a model (zero if none).
// KeyUsage is per-key (or per-model) token totals for cost/quota views (F074).
type KeyUsage struct {
	InputTok  int64   `json:"input_tokens"`
	OutputTok int64   `json:"output_tokens"`
	TotalTok  int64   `json:"total_tokens"`
	Requests  int64   `json:"requests"`
	Cost      float64 `json:"cost_usd"`
}

// keyUsage snapshots the per-key counters.
func (m *metricsCollector) keyUsage() map[string]KeyUsage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]KeyUsage, len(m.keyCounters))
	for k, c := range m.keyCounters {
		c.mu.Lock()
		out[k] = KeyUsage{InputTok: c.InputTok, OutputTok: c.OutputTok, TotalTok: c.TotalTok, Requests: c.Requests, Cost: c.Cost}
		c.mu.Unlock()
	}
	return out
}

// keyTotalTokens returns total tokens billed to one key prefix.
func (m *metricsCollector) keyTotalTokens(prefix string) int64 {
	m.mu.Lock()
	c := m.keyCounters[prefix]
	m.mu.Unlock()
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.TotalTok
}

// seedKeyUsage initializes per-key counters from persisted totals (F080).
// Only keys without a live counter are seeded, so it is safe to call once at
// startup before traffic flows.
// seedDaily restores the daily usage rollup from persisted state (F097).
func (m *metricsCollector) seedDaily(daily map[string]map[string]*store.DailyModelUsage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for day, models := range daily {
		if m.daily[day] == nil {
			m.daily[day] = map[string]*store.DailyModelUsage{}
		}
		for model, u := range models {
			if m.daily[day][model] == nil {
				cp := *u
				m.daily[day][model] = &cp
			}
		}
	}
}

// dailyUsage returns a deep copy of the daily rollup for persistence (F097).
func (m *metricsCollector) dailyUsage() map[string]map[string]*store.DailyModelUsage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]map[string]*store.DailyModelUsage, len(m.daily))
	for day, models := range m.daily {
		cp := make(map[string]*store.DailyModelUsage, len(models))
		for model, u := range models {
			c := *u
			cp[model] = &c
		}
		out[day] = cp
	}
	return out
}

// resetKeyUsage drops the per-key usage counters (F107).
func (m *metricsCollector) resetKeyUsage(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keyCounters, prefix)
}

func (m *metricsCollector) seedKeyUsage(usage map[string]KeyUsage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, u := range usage {
		if m.keyCounters[k] == nil {
			m.keyCounters[k] = &costCounter{
				InputTok:  u.InputTok,
				OutputTok: u.OutputTok,
				TotalTok:  u.TotalTok,
				Requests:  u.Requests,
				Cost:      u.Cost,
			}
		}
	}
}

// modelUsage snapshots per-model token totals with cost estimated from
// the configured pricing.
func (m *metricsCollector) modelUsage() map[string]KeyUsage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]KeyUsage, len(m.byModel))
	for k, mm := range m.byModel {
		p := m.pricingFor(k)
		out[k] = KeyUsage{
			InputTok:  mm.promptTok,
			OutputTok: mm.compTok,
			TotalTok:  mm.promptTok + mm.compTok,
			Requests:  mm.reqs,
			Cost:      float64(mm.promptTok)/1e6*p.InputPerM + float64(mm.compTok)/1e6*p.OutputPerM,
		}
	}
	return out
}

func (m *metricsCollector) lastUsedAt(model string) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastUsed[model]
}

// Snapshot renders the current metrics as JSON-ready maps.
func (m *metricsCollector) Snapshot() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	lat := latencyStats(m.latSamples)

	models := map[string]any{}
	for k, v := range m.byModel {
		name := k
		if name == "" {
			name = "(unknown)"
		}
		entry := map[string]any{
			"requests":          v.reqs,
			"errors":            v.errs,
			"prompt_tokens":     v.promptTok,
			"completion_tokens": v.compTok,
		}
		if v.tpsN > 0 {
			entry["tokens_per_sec"] = round2(v.tpsSum / float64(v.tpsN))
		}
		if v.ttftN > 0 {
			entry["ttft_avg_ms"] = round2(v.ttftSum / float64(v.ttftN))
		}
		for lk, lv := range latencyStats(v.latSamples) {
			entry["latency_"+lk] = lv
		}
		models[name] = entry
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

// latencyStats computes percentiles from a reservoir of latency samples (ms).
func latencyStats(samples []float64) map[string]any {
	out := map[string]any{}
	if len(samples) == 0 {
		return out
	}
	s := append([]float64(nil), samples...)
	sort.Float64s(s)
	out["p50_ms"] = round2(percentile(s, 0.50))
	out["p90_ms"] = round2(percentile(s, 0.90))
	out["p95_ms"] = round2(percentile(s, 0.95))
	out["p99_ms"] = round2(percentile(s, 0.99))
	out["samples"] = len(s)
	return out
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
