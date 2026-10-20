package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestChatsCRUD(t *testing.T) {
	ts, _ := newCtxTestServer(t)

	// create via POST (id empty)
	body := `{"title":"","model":"qwen","messages":[{"role":"user","content":"hello there world"},{"role":"assistant","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/api/v1/chats", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: %d %s", resp.StatusCode, b)
	}
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	id, _ := created["id"].(string)
	if len(id) != 16 {
		t.Fatalf("bad id: %v", created["id"])
	}
	if created["title"] != "hello there world" {
		t.Fatalf("title not derived: %v", created["title"])
	}

	// list
	r, _ := http.Get(ts.URL + "/api/v1/chats")
	var list []map[string]any
	json.NewDecoder(r.Body).Decode(&list)
	r.Body.Close()
	if len(list) != 1 || list[0]["messages"].(float64) != 2 {
		t.Fatalf("list: %+v", list)
	}

	// get
	r, _ = http.Get(fmt.Sprintf("%s/api/v1/chats/%s", ts.URL, id))
	var got map[string]any
	json.NewDecoder(r.Body).Decode(&got)
	r.Body.Close()
	if got["model"] != "qwen" {
		t.Fatalf("get: %+v", got)
	}

	// update via PUT (path id wins)
	upd := fmt.Sprintf(`{"id":"deadbeefdeadbeef","messages":[{"role":"user","content":"more"},{"role":"assistant","content":"ok","tools":[{"name":"web_search","query":"x"}]}]}`)
	req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/api/v1/chats/%s", ts.URL, id), bytes.NewReader([]byte(upd)))
	req.Header.Set("Content-Type", "application/json")
	ru, _ := http.DefaultClient.Do(req)
	if ru.StatusCode != 200 {
		b, _ := io.ReadAll(ru.Body)
		t.Fatalf("update: %d %s", ru.StatusCode, b)
	}
	var updated map[string]any
	json.NewDecoder(ru.Body).Decode(&updated)
	ru.Body.Close()
	if updated["id"] != id {
		t.Fatalf("path id did not win: %v", updated["id"])
	}

	// validation: empty messages, bad role filtering
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/chats", bytes.NewReader([]byte(`{"messages":[{"role":"hacker","content":"x"}]}`)))
	rv, _ := http.DefaultClient.Do(req)
	if rv.StatusCode != 400 {
		t.Errorf("all-invalid roles: %d", rv.StatusCode)
	}
	rv.Body.Close()

	// traversal / bad id rejected
	r2, _ := http.Get(ts.URL + "/api/v1/chats/..%2F..%2Fstate")
	if r2.StatusCode != 404 {
		t.Errorf("bad id: %d", r2.StatusCode)
	}
	r2.Body.Close()

	// delete
	req, _ = http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/chats/%s", ts.URL, id), nil)
	rd, _ := http.DefaultClient.Do(req)
	if rd.StatusCode != 200 {
		t.Fatalf("delete: %d", rd.StatusCode)
	}
	rd.Body.Close()
	r3, _ := http.Get(fmt.Sprintf("%s/api/v1/chats/%s", ts.URL, id))
	if r3.StatusCode != 404 {
		t.Errorf("get after delete: %d", r3.StatusCode)
	}
	r3.Body.Close()
}
