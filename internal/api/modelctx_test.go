package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"llmctl/internal/backend"
	"llmctl/internal/store"
)

func newCtxTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	t.Setenv("LLMCTL_HOME", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := backend.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	_ = reg
	srv := New(st, reg, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), 0, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func TestSetModelCtx(t *testing.T) {
	ts, st := newCtxTestServer(t)
	st.Update(func(st *store.State) error {
		st.Models["qwen"] = &store.ModelRecord{ID: "qwen", BackendID: "llama.cpp", Path: "qwen.gguf"}
		return nil
	})

	// set 8192
	resp, err := http.NewRequest("PUT", ts.URL+"/api/v1/models/qwen/ctx",
		bytes.NewReader([]byte(`{"ctxSize": 8192}`)))
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		t.Fatalf("status %d: %s", r.StatusCode, b)
	}
	r.Body.Close()

	s2, _ := st.Load()
	if s2.Models["qwen"].CtxSize != 8192 {
		t.Fatalf("ctx not persisted: %d", s2.Models["qwen"].CtxSize)
	}

	// appears in listModels
	lr, err := http.Get(ts.URL + "/api/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	var models []map[string]any
	json.NewDecoder(lr.Body).Decode(&models)
	lr.Body.Close()
	if len(models) != 1 || models[0]["ctxSize"].(float64) != 8192 {
		t.Fatalf("listModels missing ctxSize: %+v", models)
	}

	// clear with 0
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/models/qwen/ctx", bytes.NewReader([]byte(`{"ctxSize": 0}`)))
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	s3, _ := st.Load()
	if s3.Models["qwen"].CtxSize != 0 {
		t.Fatalf("ctx not cleared: %d", s3.Models["qwen"].CtxSize)
	}

	// validation: missing body field
	req, _ = http.NewRequest("PUT", ts.URL+"/api/v1/models/qwen/ctx", bytes.NewReader([]byte(`{}`)))
	r3, _ := http.DefaultClient.Do(req)
	if r3.StatusCode != 400 {
		t.Errorf("empty body: status %d", r3.StatusCode)
	}
	r3.Body.Close()
	// validation: too large
	req, _ = http.NewRequest("PUT", ts.URL+"/api/v1/models/qwen/ctx", bytes.NewReader([]byte(`{"ctxSize": 99999999}`)))
	r4, _ := http.DefaultClient.Do(req)
	if r4.StatusCode != 400 {
		t.Errorf("huge ctx: status %d", r4.StatusCode)
	}
	r4.Body.Close()
	// unknown model
	req, _ = http.NewRequest("PUT", ts.URL+"/api/v1/models/ghost/ctx", bytes.NewReader([]byte(`{"ctxSize": 4096}`)))
	r5, _ := http.DefaultClient.Do(req)
	if r5.StatusCode != 404 {
		t.Errorf("ghost model: status %d", r5.StatusCode)
	}
	r5.Body.Close()
}
