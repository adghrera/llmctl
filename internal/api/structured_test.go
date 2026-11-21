package api

import "testing"

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{`{"a":1}`, `{"a":1}`, true},
		{"Here you go:\n```json\n{\"a\": 1}\n```\nHope that helps!", `{"a": 1}`, true},
		{`prose {"x": "with } brace"} trailing`, `{"x": "with } brace"}`, true},
		{`{"nested":{"deep":[1,{"y":2}]}}`, `{"nested":{"deep":[1,{"y":2}]}}`, true},
		{`no json here`, "", false},
		{`{"unterminated":`, "", false},
		{`{"esc":"\""}`, `{"esc":"\""}`, true},
	}
	for _, c := range cases {
		got, ok := extractJSON(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("extractJSON(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
