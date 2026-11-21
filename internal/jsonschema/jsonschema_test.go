package jsonschema

import (
	"encoding/json"
	"testing"
)

func mustSchema(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func mustVal(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValidObject(t *testing.T) {
	s := mustSchema(t, `{"type":"object","required":["name","age"],"properties":{"name":{"type":"string"},"age":{"type":"integer"},"tags":{"type":"array","items":{"type":"string"}}}}`)
	if err := Validate(s, mustVal(t, `{"name":"x","age":3,"tags":["a","b"]}`)); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if err := Validate(s, mustVal(t, `{"name":"x","age":3}`)); err != nil {
		t.Fatalf("optional prop should pass: %v", err)
	}
}

func TestMissingRequired(t *testing.T) {
	s := mustSchema(t, `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`)
	err := Validate(s, mustVal(t, `{"age":1}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != `$: missing required property "name"` {
		t.Fatalf("msg = %q", got)
	}
}

func TestWrongTypes(t *testing.T) {
	s := mustSchema(t, `{"type":"object","properties":{"n":{"type":"number"},"i":{"type":"integer"},"b":{"type":"boolean"}}}`)
	if err := Validate(s, mustVal(t, `{"n":3.5,"i":3,"b":true}`)); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := Validate(s, mustVal(t, `{"n":3,"i":3,"b":true}`)); err != nil {
		t.Fatalf("integer is a number: %v", err)
	}
	if err := Validate(s, mustVal(t, `{"i":3.5}`)); err == nil {
		t.Fatal("3.5 is not integer")
	}
	if err := Validate(s, mustVal(t, `{"b":"yes"}`)); err == nil {
		t.Fatal("string is not boolean")
	}
}

func TestNestedAndArrays(t *testing.T) {
	s := mustSchema(t, `{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","required":["id"],"properties":{"id":{"type":"integer"}}}}}}`)
	if err := Validate(s, mustVal(t, `{"items":[{"id":1},{"id":2}]}`)); err != nil {
		t.Fatalf("valid: %v", err)
	}
	err := Validate(s, mustVal(t, `{"items":[{"id":1},{"x":2}]}`))
	if err == nil {
		t.Fatal("expected nested error")
	}
	if got := err.Error(); got != "$.items[1]: missing required property \"id\"" {
		t.Fatalf("msg = %q", got)
	}
}

func TestEnum(t *testing.T) {
	s := mustSchema(t, `{"type":"string","enum":["red","green"]}`)
	if err := Validate(s, "red"); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := Validate(s, "blue"); err == nil {
		t.Fatal("expected enum error")
	}
}

func TestEmptySchemaAccepts(t *testing.T) {
	if err := Validate(map[string]any{}, mustVal(t, `{"anything":true}`)); err != nil {
		t.Fatalf("empty schema should accept: %v", err)
	}
}
