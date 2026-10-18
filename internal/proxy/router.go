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

// nextReplica rotates through matches round-robin, keyed by model id.
func (rt *Router) nextReplica(key string, matches []*store.InstanceRecord) *store.InstanceRecord {
	if len(matches) == 1 {
		return matches[0]
	}
	rt.rrMu.Lock()
	counter := rt.rrCount[key]
	if counter == nil {
		counter = &atomic.Uint64{}
		rt.rrCount[key] = counter
	}
	rt.rrMu.Unlock()
	idx := counter.Add(1) % uint64(len(matches))
	return matches[idx]
}

// Resolve maps a requested model name to an upstream target.
// Matching order: exact model id match (round-robin over replicas) >
// prefix match > single running instance as default when model is empty.
func (rt *Router) Resolve(model string) (*Target, error) {
	insts := rt.RunningInstances()
	if len(insts) == 0 {
		return nil, ErrNoRunning
	}

	var chosen *store.InstanceRecord
	if model != "" {
		var exact []*store.InstanceRecord
		for _, inst := range insts {
			if strings.EqualFold(inst.ModelID, model) {
				exact = append(exact, inst)
			}
		}
		if len(exact) > 0 {
			chosen = rt.nextReplica(strings.ToLower(model), exact)
		}
		if chosen == nil {
			var prefix []*store.InstanceRecord
			for _, inst := range insts {
				if strings.HasPrefix(strings.ToLower(model), strings.ToLower(inst.ModelID)) {
					prefix = append(prefix, inst)
				}
			}
			if len(prefix) > 0 {
				chosen = rt.nextReplica("prefix:"+strings.ToLower(model), prefix)
			}
		}
	}
	if chosen == nil && len(insts) == 1 && model == "" {
		chosen = insts[0]
	}
	if chosen == nil {
		avail := make([]string, 0, len(insts))
		for _, i := range insts {
			avail = append(avail, i.ModelID)
		}
		sort.Strings(avail)
		return nil, fmt.Errorf("model %q not served by any running instance (available: %s)", model, strings.Join(avail, ", "))
	}
	return targetFor(chosen, rt.sessionFor)
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
