package proxy

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"llmctl/internal/backend"
	"llmctl/internal/stdio"
	"llmctl/internal/store"
)

// ErrNoRunning is returned when no instance can serve a request.
var ErrNoRunning = errors.New("no running instances; start one with: llmctl start <model-id>")

// Target is a resolved upstream endpoint for one request. Exactly one of
// BaseURL (http) or Session (stdio) is set, selected by Transport.
type Target struct {
	InstanceID string
	Transport  string // "http" | "stdio"
	BaseURL    *url.URL
	APIPath    string // e.g. "/v1" (http only)
	Session    *stdio.Session
}

// Router resolves model names to running instances. It re-reads state.json
// lazily (mtime-checked), so `llmctl start` from another process is picked
// up by the running gateway without IPC.
//
// When several instances serve the same model (replicas), requests are
// distributed round-robin across them.
type Router struct {
	mu         sync.RWMutex
	cached     *store.State
	cacheMod   int64
	load       func() (*store.State, int64, error)
	sessionFor func(instanceID string) *stdio.Session

	rrMu    sync.Mutex
	rrCount map[string]*atomic.Uint64 // model id -> next-replica index
}

// NewRouter builds a router over the given state loader
// (state, modtime-unix-nano) and an optional stdio session resolver.
func NewRouter(load func() (*store.State, int64, error), sessionFor func(instanceID string) *stdio.Session) *Router {
	return &Router{load: load, sessionFor: sessionFor, rrCount: map[string]*atomic.Uint64{}}
}

func (rt *Router) state() (*store.State, error) {
	st, mod, err := rt.load()
	if err != nil {
		return nil, err
	}
	rt.mu.Lock()
	if rt.cached == nil || rt.cacheMod != mod {
		rt.cached = st
		rt.cacheMod = mod
	}
	c := rt.cached
	rt.mu.Unlock()
	return c, nil
}

// RunningInstances lists instances marked running.
func (rt *Router) RunningInstances() []*store.InstanceRecord {
	st, err := rt.state()
	if err != nil {
		return nil
	}
	out := make([]*store.InstanceRecord, 0, len(st.Instances))
	for _, id := range st.SortedInstanceIDs() {
		if inst := st.Instances[id]; inst.Status == store.StatusRunning {
			out = append(out, inst)
		}
	}
	return out
}

// Aliases returns the gateway alias -> model id map (F065).
func (rt *Router) Aliases() map[string]string {
	st, err := rt.state()
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(st.Aliases))
	for k, v := range st.Aliases {
		out[k] = v
	}
	return out
}

// Resolve maps a requested model name to an upstream target.
// Matching order: alias (one hop, F065) > exact model id match (round-robin
// over replicas) > prefix match > single running instance as default when
// model is empty.
func (rt *Router) Resolve(model string) (*Target, error) {
	targets, err := rt.ResolveList(model)
	if err != nil {
		return nil, err
	}
	return targets[0], nil
}

// ResolveList returns every matching target, best first (F070 failover).
// The round-robin rotation picks the starting replica; the rest follow in
// wrap-around order so a failed request can try the next replica.
func (rt *Router) ResolveList(model string) ([]*Target, error) {
	if model != "" {
		if st, err := rt.state(); err == nil {
			if target, ok := st.Aliases[strings.ToLower(model)]; ok && strings.TrimSpace(target) != "" {
				model = target
			}
		}
	}
	insts := rt.RunningInstances()
	if len(insts) == 0 {
		return nil, ErrNoRunning
	}

	var matches []*store.InstanceRecord
	if model != "" {
		for _, inst := range insts {
			if strings.EqualFold(inst.ModelID, model) {
				matches = append(matches, inst)
			}
		}
		if len(matches) == 0 {
			for _, inst := range insts {
				if strings.HasPrefix(strings.ToLower(model), strings.ToLower(inst.ModelID)) {
					matches = append(matches, inst)
				}
			}
		}
	}
	if len(matches) == 0 && len(insts) == 1 && model == "" {
		matches = insts
	}
	if len(matches) == 0 {
		avail := make([]string, 0, len(insts))
		for _, i := range insts {
			avail = append(avail, i.ModelID)
		}
		sort.Strings(avail)
		return nil, fmt.Errorf("model %q not served by any running instance (available: %s)", model, strings.Join(avail, ", "))
	}

	// Rotate so replicas take turns being tried first.
	key := strings.ToLower(model)
	if len(matches) > 1 {
		rt.rrMu.Lock()
		counter := rt.rrCount[key]
		if counter == nil {
			counter = &atomic.Uint64{}
			rt.rrCount[key] = counter
		}
		rt.rrMu.Unlock()
		start := int(counter.Add(1) % uint64(len(matches)))
		rotated := make([]*store.InstanceRecord, 0, len(matches))
		rotated = append(rotated, matches[start:]...)
		rotated = append(rotated, matches[:start]...)
		matches = rotated
	}

	targets := make([]*Target, 0, len(matches))
	for _, inst := range matches {
		t, err := targetFor(inst, rt.sessionFor)
		if err != nil {
			continue // skip instances that cannot produce a target
		}
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no usable target for model %q", model)
	}
	return targets, nil
}

func targetFor(inst *store.InstanceRecord, sessionFor func(string) *stdio.Session) (*Target, error) {
	if inst.Transport == backend.TransportStdio {
		var sess *stdio.Session
		if sessionFor != nil {
			sess = sessionFor(inst.ID)
		}
		if sess == nil {
			return nil, fmt.Errorf("stdio session for instance %q is not live (it may be starting)", inst.ID)
		}
		return &Target{InstanceID: inst.ID, Transport: backend.TransportStdio, Session: sess}, nil
	}
	base, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", inst.Port))
	if err != nil {
		return nil, err
	}
	api := inst.APIBasePath
	if api == "" {
		api = "/v1"
	}
	return &Target{InstanceID: inst.ID, Transport: backend.TransportHTTP, BaseURL: base, APIPath: api}, nil
}
