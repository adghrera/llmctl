package supervisor

import (
	"testing"

	"llmctl/internal/store"
)

func TestApplyModelCtxDefault(t *testing.T) {
	m := &store.ModelRecord{ID: "m", CtxSize: 16384}

	// no vars at all -> model default applied
	if got := applyModelCtxDefault(nil, m)["ctx"]; got != "16384" {
		t.Errorf("nil vars: ctx = %q", got)
	}
	// explicit ctx wins
	vars := map[string]string{"ctx": "4096", "ngl": "99"}
	got := applyModelCtxDefault(vars, m)
	if got["ctx"] != "4096" || got["ngl"] != "99" {
		t.Errorf("explicit ctx overridden: %+v", got)
	}
	// input map not mutated
	if applyModelCtxDefault(nil, m) == nil {
		t.Fatal("expected non-nil map")
	}
	// model with no default leaves vars alone
	if got := applyModelCtxDefault(nil, &store.ModelRecord{ID: "m"}); got != nil {
		t.Errorf("expected nil passthrough, got %+v", got)
	}
	if got := applyModelCtxDefault(map[string]string{"ngl": "1"}, &store.ModelRecord{CtxSize: 0}); got["ctx"] != "" {
		t.Errorf("zero CtxSize injected ctx: %+v", got)
	}
	// original vars map untouched by the copy path
	orig := map[string]string{"ngl": "1"}
	_ = applyModelCtxDefault(orig, m)
	if _, ok := orig["ctx"]; ok {
		t.Error("input map was mutated")
	}
}
