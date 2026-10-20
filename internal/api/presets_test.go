package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"llmctl/internal/store"
)

func TestPresetsCRUD(t *testing.T) {
	ts, st := newCtxTestServer(t)
	st.Update(func(st *store.State) error {
		st.Models["qwen"] = &store.ModelRecord{ID: "qwen", BackendID: "llama.cpp", Path: "qwen.gguf"}
		return nil
	})

	// save with numeric vars + unknown model rejection
	resp, err := http.Post(ts.URL+"/api/v1/presets", "application/json",
		bytes.NewReader([]byte(`{"name":"big-ctx","modelId":"ghost","vars":{"ctx": 32768}}`)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("unknown model: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Post(ts.URL+"/api/v1/presets", "application/json",
		bytes.NewReader([]byte(`{"name":"big-ctx","modelId":"qwen","backendId":"llama.cpp","port":9090,"vars":{"ctx": 32768, "ngl": "99"}}`)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("save: status %d %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	// list shows it with coerced vars
	r, err := http.Get(ts.URL + "/api/v1/presets")
	if err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	json.NewDecoder(r.Body).Decode(&list)
	r.Body.Close()
	if len(list) != 1 || list[0]["name"] != "big-ctx" {
		t.Fatalf("list: %+v", list)
	}
	vars := list[0]["vars"].(map[string]any)
	if vars["ctx"] != "32768" || vars["ngl"] != "99" {
		t.Fatalf("vars coercion: %+v", vars)
	}

	// replace same name (upsert)
	resp, _ = http.Post(ts.URL+"/api/v1/presets", "application/json",
		bytes.NewReader([]byte(`{"name":"big-ctx","modelId":"qwen","vars":{"ctx":"8192"}}`)))
	if resp.StatusCode != 200 {
		t.Fatalf("upsert: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	r, _ = http.Get(ts.URL + "/api/v1/presets")
	list = nil
	json.NewDecoder(r.Body).Decode(&list)
	r.Body.Close()
	if len(list) != 1 {
		t.Fatalf("upsert created dup: %+v", list)
	}

	// validation: missing name / bad name chars
	resp, _ = http.Post(ts.URL+"/api/v1/presets", "application/json", bytes.NewReader([]byte(`{"modelId":"qwen"}`)))
	if resp.StatusCode != 400 {
		t.Errorf("no name: %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp, _ = http.Post(ts.URL+"/api/v1/presets", "application/json", bytes.NewReader([]byte(`{"name":"a/b","modelId":"qwen"}`)))
	if resp.StatusCode != 400 {
		t.Errorf("bad name: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// start unknown preset -> 404
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/presets/ghost/start", nil)
	rs, _ := http.DefaultClient.Do(req)
	if rs.StatusCode != 404 {
		t.Errorf("start ghost: %d", rs.StatusCode)
	}
	rs.Body.Close()

	// delete
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/v1/presets/big-ctx", nil)
	rd, _ := http.DefaultClient.Do(req)
	if rd.StatusCode != 200 {
		t.Fatalf("delete: %d", rd.StatusCode)
	}
	rd.Body.Close()
	r, _ = http.Get(ts.URL + "/api/v1/presets")
	list = nil
	json.NewDecoder(r.Body).Decode(&list)
	r.Body.Close()
	if len(list) != 0 {
		t.Fatalf("after delete: %+v", list)
	}
	// delete again -> 404
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/v1/presets/big-ctx", nil)
	rd, _ = http.DefaultClient.Do(req)
	if rd.StatusCode != 404 {
		t.Errorf("double delete: %d", rd.StatusCode)
	}
	rd.Body.Close()
}
