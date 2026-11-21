package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostDeliversJSON(t *testing.T) {
	got := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		var v map[string]any
		json.NewDecoder(r.Body).Decode(&v)
		got <- v
		w.WriteHeader(200)
	}))
	defer srv.Close()

	if err := Post(context.Background(), srv.URL, map[string]any{"event": "x", "n": 1}); err != nil {
		t.Fatal(err)
	}
	v := <-got
	if v["event"] != "x" {
		t.Errorf("payload = %v", v)
	}
}

func TestPostRejectsNonHTTP(t *testing.T) {
	if err := Post(context.Background(), "ftp://x/y", nil); err == nil {
		t.Error("expected error for non-http url")
	}
}

func TestPostErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	if err := Post(context.Background(), srv.URL, map[string]any{}); err == nil {
		t.Error("expected error for 500 response")
	}
}
