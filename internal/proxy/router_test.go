package proxy

import (
	"testing"

	"llmctl/internal/store"
)

func TestRouterRoundRobinReplicas(t *testing.T) {
	st := &store.State{
		Backends: map[string]*store.BackendRecord{},
		Models:   map[string]*store.ModelRecord{},
		Instances: map[string]*store.InstanceRecord{
			"a": {ID: "a", ModelID: "m", BackendID: "fake", Port: 1001, Status: store.StatusRunning, APIBasePath: "/v1"},
			"b": {ID: "b", ModelID: "m", BackendID: "fake", Port: 1002, Status: store.StatusRunning, APIBasePath: "/v1"},
			"c": {ID: "c", ModelID: "m", BackendID: "fake", Port: 1003, Status: store.StatusRunning, APIBasePath: "/v1"},
		},
	}
	rt := NewRouter(func() (*store.State, int64, error) { return st, 1, nil }, nil)

	seen := map[string]int{}
	for i := 0; i < 9; i++ {
		tgt, err := rt.Resolve("m")
		if err != nil {
			t.Fatal(err)
		}
		seen[tgt.InstanceID]++
	}
	// Round-robin over 3 replicas x 3 rounds = exactly 3 each.
	for _, id := range []string{"a", "b", "c"} {
		if seen[id] != 3 {
			t.Fatalf("replica %s got %d requests, want 3 (seen=%v)", id, seen[id], seen)
		}
	}
}

func TestRouterResolveListFailoverOrder(t *testing.T) {
	st := &store.State{
		Backends: map[string]*store.BackendRecord{},
		Models:   map[string]*store.ModelRecord{},
		Instances: map[string]*store.InstanceRecord{
			"a": {ID: "a", ModelID: "m", BackendID: "fake", Port: 1001, Status: store.StatusRunning, APIBasePath: "/v1"},
			"b": {ID: "b", ModelID: "m", BackendID: "fake", Port: 1002, Status: store.StatusRunning, APIBasePath: "/v1"},
			"c": {ID: "c", ModelID: "m", BackendID: "fake", Port: 1003, Status: store.StatusStopped, APIBasePath: "/v1"},
		},
	}
	rt := NewRouter(func() (*store.State, int64, error) { return st, 1, nil }, nil)
	// Every list must contain both running replicas and never the stopped one,
	// with the first entry rotating across requests (F070).
	firsts := map[string]int{}
	for i := 0; i < 6; i++ {
		ts, err := rt.ResolveList("m")
		if err != nil {
			t.Fatal(err)
		}
		if len(ts) != 2 {
			t.Fatalf("got %d targets, want 2", len(ts))
		}
		ids := map[string]bool{ts[0].InstanceID: true, ts[1].InstanceID: true}
		if ids["c"] || !ids["a"] || !ids["b"] {
			t.Fatalf("bad replica set: %v, %v", ts[0].InstanceID, ts[1].InstanceID)
		}
		firsts[ts[0].InstanceID]++
	}
	if firsts["a"] == 0 || firsts["b"] == 0 {
		t.Fatalf("starting replica never rotated: %v", firsts)
	}
}

func TestRouterSkipsStoppedReplicas(t *testing.T) {
	st := &store.State{
		Backends: map[string]*store.BackendRecord{},
		Models:   map[string]*store.ModelRecord{},
		Instances: map[string]*store.InstanceRecord{
			"a": {ID: "a", ModelID: "m", BackendID: "fake", Port: 1001, Status: store.StatusRunning, APIBasePath: "/v1"},
			"b": {ID: "b", ModelID: "m", BackendID: "fake", Port: 1002, Status: store.StatusStopped, APIBasePath: "/v1"},
		},
	}
	rt := NewRouter(func() (*store.State, int64, error) { return st, 1, nil }, nil)
	for i := 0; i < 4; i++ {
		tgt, err := rt.Resolve("m")
		if err != nil {
			t.Fatal(err)
		}
		if tgt.InstanceID != "a" {
			t.Fatalf("routed to stopped replica %q", tgt.InstanceID)
		}
	}
}

func TestRouterAliasResolution(t *testing.T) {
	st := &store.State{
		Backends: map[string]*store.BackendRecord{},
		Models:   map[string]*store.ModelRecord{},
		Instances: map[string]*store.InstanceRecord{
			"a": {ID: "a", ModelID: "big-model", BackendID: "fake", Port: 1001, Status: store.StatusRunning, APIBasePath: "/v1"},
		},
		Aliases: map[string]string{"chat": "big-model"},
	}
	rt := NewRouter(func() (*store.State, int64, error) { return st, 1, nil }, nil)

	tgt, err := rt.Resolve("chat")
	if err != nil {
		t.Fatal(err)
	}
	if tgt.InstanceID != "a" {
		t.Fatalf("alias resolved to %s, want a", tgt.InstanceID)
	}
	// Case-insensitive alias lookup.
	if _, err := rt.Resolve("Chat"); err != nil {
		t.Fatalf("case-insensitive alias failed: %v", err)
	}
	// Unknown alias still falls through to normal matching (and fails).
	if _, err := rt.Resolve("nope"); err == nil {
		t.Fatal("expected error for unknown model")
	}
}
