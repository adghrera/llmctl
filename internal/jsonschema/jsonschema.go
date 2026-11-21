// Package jsonschema implements a small JSON Schema validator covering the
// subset llmctl needs for structured output: type (object/string/number/
// integer/boolean/array/null), enum, required, properties, items.
// Unknown keywords are ignored (forward-compatible).
package jsonschema

import (
	"fmt"
	"strings"
)

// Validate checks value (decoded JSON) against schema and returns the first
// violation found, or nil.
func Validate(schema map[string]any, value any) error {
	return validate(schema, value, "$")
}

func validate(schema map[string]any, value any, path string) error {
	if t, ok := schema["type"]; ok {
		types, err := typeList(t)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !matchesAnyType(types, value) {
			return fmt.Errorf("%s: expected type %s, got %s", path, strings.Join(types, "|"), jsonType(value))
		}
	}
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if fmt.Sprint(e) == fmt.Sprint(value) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: value %v not in enum", path, value)
		}
	}
	switch v := value.(type) {
	case map[string]any:
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				name, _ := r.(string)
				if _, has := v[name]; !has {
					return fmt.Errorf("%s: missing required property %q", path, name)
				}
			}
		}
		if props, ok := schema["properties"].(map[string]any); ok {
			for name, sub := range props {
				subSchema, ok := sub.(map[string]any)
				if !ok {
					continue
				}
				if child, has := v[name]; has {
					if err := validate(subSchema, child, path+"."+name); err != nil {
						return err
					}
				}
			}
		}
	case []any:
		if items, ok := schema["items"].(map[string]any); ok {
			for i, el := range v {
				if err := validate(items, el, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func typeList(t any) ([]string, error) {
	switch v := t.(type) {
	case string:
		return []string{v}, nil
	case []any:
		var out []string
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("invalid type entry %v", e)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("invalid type %v", t)
}

func matchesAnyType(types []string, value any) bool {
	got := jsonType(value)
	for _, t := range types {
		if t == got {
			return true
		}
		// integer is a subtype of number.
		if t == "number" && got == "integer" {
			return true
		}
	}
	return false
}

func jsonType(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		if x == float64(int64(x)) {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}
