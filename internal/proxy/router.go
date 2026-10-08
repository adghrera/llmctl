package proxy

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"llmctl/internal/store"
)

// ErrNoRunning is returned when no instance can serve a request.
var ErrNoRunning = errors.New("no running instances; start one with: llmctl start <model-id>")

// Target is a resolved upstream endpoint for one request.
type Target struct {
	InstanceID string
	BaseURL    *url.URL
	APIPath    string // e.g. "/v1"
}

// Router resolves model names to running instances. It re-reads state.json
// lazily (mtime-checked), so `llmctl start` from another process is picked
// up by the running gateway without IPC.
type Router struct {
	mu       sync.RWMutex
	staleOK  bool
	cached   *store.State
	cacheMod int64
	load     func() (*store.State, int64, error)
}

// NewRouter builds a router over the given state loader
// (state, modtime-unix-nano).
func NewRouter(load func() (*store.State, int64, error)) *Router {
	return &Router{load: load}
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

// RunningInstances lists instances marked running with a live PID check
// done by the daemon's reaper; here we trust status.
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

// Resolve maps a requested model name to an upstream target.
// Matching order: exact model id match on an instance > instance whose
// upstream advertises the name (cached) > single running instance as
// default when model is empty > prefix match.
func (rt *Router) Resolve(model string) (*Target, error) {
	insts := rt.RunningInstances()
	if len(insts) == 0 {
		return nil, ErrNoRunning
	}

	// Exact: instance's model id or alias.
	if model != "" {
		for _, inst := range insts {
			if strings.EqualFold(inst.ModelID, model) {
				return targetFor(inst)
			}
		}
		// Prefix: "qwen2.5-7b-instruct-q4:latest" style or alias prefix.
		for _, inst := range insts {
			if strings.HasPrefix(strings.ToLower(model), strings.ToLower(inst.ModelID)) {
				return targetFor(inst)
			}
		}
	}
	if len(insts) == 1 && model == "" {
		return targetFor(insts[0])
	}
	// Multiple instances, no match: list what's available.
	avail := make([]string, 0, len(insts))
	for _, i := range insts {
		avail = append(avail, i.ModelID)
	}
	sort.Strings(avail)
	return nil, fmt.Errorf("model %q not served by any running instance (available: %s)", model, strings.Join(avail, ", "))
}

func targetFor(inst *store.InstanceRecord) (*Target, error) {
	base, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", inst.Port))
	if err != nil {
		return nil, err
	}
	api := inst.APIBasePath
	if api == "" {
		api = "/v1"
	}
	return &Target{InstanceID: inst.ID, BaseURL: base, APIPath: api}, nil
}
