package api

import (
	"encoding/json"
	"testing"
)

func TestCoerceVars(t *testing.T) {
	var v any
	json.Unmarshal([]byte(`{"ctx": 64000, "ngl": "99", "temp": 0.7, "flash": true}`), &v)
	m, err := coerceVars(v)
	if err != nil {
		t.Fatal(err)
	}
	if m["ctx"] != "64000" || m["ngl"] != "99" || m["temp"] != "0.7" || m["flash"] != "true" {
		t.Fatalf("coercion wrong: %+v", m)
	}
	// nil passthrough
	if got, err := coerceVars(nil); got != nil || err != nil {
		t.Fatalf("nil: %+v %v", got, err)
	}
	// non-object rejected
	if _, err := coerceVars("ctx=64000"); err == nil {
		t.Error("string vars accepted")
	}
	// nested value rejected
	var nested any
	json.Unmarshal([]byte(`{"a": {"b": 1}}`), &nested)
	if _, err := coerceVars(nested); err == nil {
		t.Error("nested vars accepted")
	}
}
