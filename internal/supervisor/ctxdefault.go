package supervisor

import (
	"strconv"

	"llmctl/internal/store"
)

// applyModelCtxDefault fills vars["ctx"] from the model's configured
// context size when the caller did not set one explicitly.
// model.CtxSize == 0 means "no per-model default" (backend picks).
func applyModelCtxDefault(vars map[string]string, model *store.ModelRecord) map[string]string {
	if model == nil || model.CtxSize <= 0 {
		return vars
	}
	if _, ok := vars["ctx"]; ok {
		return vars
	}
	out := make(map[string]string, len(vars)+1)
	for k, v := range vars {
		out[k] = v
	}
	out["ctx"] = strconv.Itoa(model.CtxSize)
	return out
}
