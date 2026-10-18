package supervisor

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"llmctl/internal/backend"
	"llmctl/internal/store"
)

func newTestSupervisor(t *testing.T) *Supervisor {
	t.Setenv("LLMCTL_HOME", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := backend.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return New(st, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestAutoStartSpecsRoundTrip(t *testing.T) {
	sv := newTestSupervisor(t)

	// SetAutoStart rejects unknown models.
	if err := sv.SetAutoStart(store.AutoStartSpec{ModelID: "ghost"}); err == nil {
		t.Fatal("expected error for unknown model")
	}

	// Register a model in the store.
	if err := sv.store.Update(func(s *store.State) error {
		s.Models["m1"] = &store.ModelRecord{ID: "m1", BackendID: "llama.cpp", Path: "m1.gguf"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := sv.SetAutoStart(store.AutoStartSpec{ModelID: "m1", Name: "m1-auto", Port: 9999}); err != nil {
		t.Fatal(err)
	}
	specs, err := sv.AutoStartSpecs()
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs["m1"].Port != 9999 || specs["m1"].Name != "m1-auto" {
		t.Fatalf("specs = %+v", specs)
	}

	// Replace.
	if err := sv.SetAutoStart(store.AutoStartSpec{ModelID: "m1", Port: 1234}); err != nil {
		t.Fatal(err)
	}
	specs, _ = sv.AutoStartSpecs()
	if specs["m1"].Port != 1234 {
		t.Fatalf("replace failed: %+v", specs["m1"])
	}

	// Clear.
	if err := sv.ClearAutoStart("m1"); err != nil {
		t.Fatal(err)
	}
	specs, _ = sv.AutoStartSpecs()
	if len(specs) != 0 {
		t.Fatalf("clear failed: %+v", specs)
	}
}

func TestAutoStartAllSkipsFailures(t *testing.T) {
	sv := newTestSupervisor(t)
	// Spec for a model whose backend is not installed: AutoStartAll must
	// log-and-continue, not panic or block.
	if err := sv.store.Update(func(s *store.State) error {
		s.Models["m1"] = &store.ModelRecord{ID: "m1", BackendID: "nope", Path: "m1.gguf"}
		s.AutoStart = map[string]*store.AutoStartSpec{
			"m1": {ModelID: "m1"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sv.AutoStartAll(context.Background()) // must return promptly without panic
	// Nothing should be running.
	st, _ := sv.store.Load()
	if len(st.Instances) != 0 {
		t.Fatalf("expected no instances, got %d", len(st.Instances))
	}
}
