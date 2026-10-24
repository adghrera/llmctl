// Package cost tracks token usage and estimates costs per API key and per
// model. Pricing is based on per-1M-token rates for common model tiers.
package cost

import (
	"sync"
	"time"
)

// ModelPricing is the per-1M-token cost for a model tier.
type ModelPricing struct {
	InputPerM  float64 // cost per 1M input tokens
	OutputPerM float64 // cost per 1M output tokens
}

// Default pricing for common model tiers (per 1M tokens, in USD).
// These are estimates; actual pricing varies by provider and model.
var DefaultPricing = map[string]ModelPricing{
	"llama.cpp":   {InputPerM: 0.00, OutputPerM: 0.00},   // local, free
	"ref-stdio":   {InputPerM: 0.00, OutputPerM: 0.00},   // local, free
	"ollama":      {InputPerM: 0.00, OutputPerM: 0.00},   // local, free
	"vllm":        {InputPerM: 0.00, OutputPerM: 0.00},   // local, free
	"custom":      {InputPerM: 0.00, OutputPerM: 0.00},   // user-defined
	"gpt-3.5":     {InputPerM: 0.50, OutputPerM: 1.50},
	"gpt-4":       {InputPerM: 5.00, OutputPerM: 15.00},
	"claude-3":    {InputPerM: 3.00, OutputPerM: 15.00},
	"default":     {InputPerM: 1.00, OutputPerM: 3.00},
}

// Counter tracks token usage and cost for a key or model.
type Counter struct {
	mu        sync.Mutex
	InputTok  int64   `json:"input_tokens"`
	OutputTok int64   `json:"output_tokens"`
	TotalTok  int64   `json:"total_tokens"`
	Requests  int64   `json:"requests"`
	Cost      float64 `json:"cost_usd"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (c *Counter) add(input, output int64, pricing ModelPricing) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.InputTok += input
	c.OutputTok += output
	c.TotalTok += input + output
	c.Requests++
	inputCost := float64(input) / 1e6 * pricing.InputPerM
	outputCost := float64(output) / 1e6 * pricing.OutputPerM
	c.Cost += inputCost + outputCost
	c.UpdatedAt = time.Now().UTC()
}

// Tracker tracks token usage and cost per key and per model.
type Tracker struct {
	mu      sync.Mutex
	keys    map[string]*Counter // key prefix -> counter
	models  map[string]*Counter // model id -> counter
	pricing map[string]ModelPricing
}

func NewTracker() *Tracker {
	return &Tracker{
		keys:    make(map[string]*Counter),
		models:  make(map[string]*Counter),
		pricing: make(map[string]ModelPricing),
	}
}

func (t *Tracker) SetPricing(model string, p ModelPricing) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pricing[model] = p
}

func (t *Tracker) pricingFor(model string) ModelPricing {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p, ok := t.pricing[model]; ok {
		return p
	}
	return DefaultPricing["default"]
}

func (t *Tracker) addKey(keyPrefix string, input, output int64, model string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.keys[keyPrefix]
	if !ok {
		c = &Counter{}
		t.keys[keyPrefix] = c
	}
	c.add(input, output, t.pricingFor(model))

	m, ok := t.models[model]
	if !ok {
		m = &Counter{}
		t.models[model] = m
	}
	m.add(input, output, t.pricingFor(model))
}

// KeySnapshot returns a copy of all key counters.
func (t *Tracker) KeySnapshot() map[string]*Counter {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]*Counter, len(t.keys))
	for k, c := range t.keys {
		c.mu.Lock()
		out[k] = &Counter{
			InputTok:  c.InputTok,
			OutputTok: c.OutputTok,
			TotalTok:  c.TotalTok,
			Requests:  c.Requests,
			Cost:      c.Cost,
			UpdatedAt: c.UpdatedAt,
		}
		c.mu.Unlock()
	}
	return out
}

// ModelSnapshot returns a copy of all model counters.
func (t *Tracker) ModelSnapshot() map[string]*Counter {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]*Counter, len(t.models))
	for k, c := range t.models {
		c.mu.Lock()
		out[k] = &Counter{
			InputTok:  c.InputTok,
			OutputTok: c.OutputTok,
			TotalTok:  c.TotalTok,
			Requests:  c.Requests,
			Cost:      c.Cost,
			UpdatedAt: c.UpdatedAt,
		}
		c.mu.Unlock()
	}
	return out
}

// Totals returns combined totals across all keys and models.
func (t *Tracker) Totals() (input, output, total int64, cost float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.keys {
		c.mu.Lock()
		input += c.InputTok
		output += c.OutputTok
		total += c.TotalTok
		cost += c.Cost
		c.mu.Unlock()
	}
	return
}
