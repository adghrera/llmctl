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
