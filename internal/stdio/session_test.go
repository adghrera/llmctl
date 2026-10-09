package stdio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// buildFakeBackend compiles an inline llmctl-stdio/1 echo backend to a temp
// binary, so the test exercises the real wire protocol (hello, request,
// chunk, done) without an external dependency.
func buildFakeBackend(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prog := `package main
import ("bufio";"encoding/json";"fmt";"os";"strings")
func main(){
  fmt.Println(` + "`" + `{"protocol":"llmctl-stdio/1","name":"fake","models":["fake-model"]}` + "`" + `)
  sc:=bufio.NewScanner(os.Stdin); sc.Buffer(make([]byte,64*1024),4*1024*1024)
  for sc.Scan(){
    var req struct{ ID string; Method string; Params map[string]any }
    if json.Unmarshal(sc.Bytes(),&req)!=nil { continue }
    if req.Method=="ping"{ fmt.Printf(` + "`" + `{"id":"%s","type":"pong"}` + "`" + `+"\n", req.ID); continue }
    user:=""
    if msgs,ok:=req.Params["messages"].([]any);ok{
      for _,m:=range msgs{ if mm,ok:=m.(map[string]any);ok && mm["role"]=="user"{ user=fmt.Sprint(mm["content"]) } }
    }
    for _,tok:=range strings.Split("echo:"+user," "){
      chunk:=map[string]any{"object":"chat.completion.chunk","choices":[]any{map[string]any{"delta":map[string]string{"content":tok+" "}}}}
      b,_:=json.Marshal(chunk)
      fmt.Printf(` + "`" + `{"id":"%s","type":"chunk","data":%s}` + "`" + `+"\n", req.ID, b)
    }
    fmt.Printf(` + "`" + `{"id":"%s","type":"done","data":{}}` + "`" + `+"\n", req.ID)
  }
}`
	if err := os.WriteFile(dir+"/main.go", []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/go.mod", []byte("module fakebackend\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := dir + "/fake-backend"
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake backend: %v\n%s", err, out)
	}
	return bin
}

func TestSessionHelloAndCall(t *testing.T) {
	bin := buildFakeBackend(t)
	sess, hello, err := Launch(LaunchConfig{Binary: bin, ReadyTimeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer sess.Close()
	if hello.Protocol != Protocol {
		t.Fatalf("bad protocol: %q", hello.Protocol)
	}
	if hello.Name != "fake" {
		t.Fatalf("bad name: %q", hello.Name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	msgs, err := sess.Call(ctx, "req-1", "chat.completions", map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hello world"}},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var got []string
	terminal := ""
	for m := range msgs {
		switch m.Type {
		case "chunk":
			var c struct {
				Choices []struct {
					Delta struct{ Content string } `json:"delta"`
				} `json:"choices"`
			}
			json.Unmarshal(m.Data, &c)
			got = append(got, c.Choices[0].Delta.Content)
		case "done":
			terminal = "done"
		case "error":
			terminal = "error"
		}
	}
	if terminal != "done" {
		t.Fatalf("terminal=%q", terminal)
	}
	if len(got) == 0 {
		t.Fatal("no chunks received")
	}
	if !strings.Contains(strings.Join(got, ""), "echo:hello") {
		t.Fatalf("unexpected chunks: %v", got)
	}
}

// TestConcurrentCalls proves many requests can be in flight over one pipe.
func TestConcurrentCalls(t *testing.T) {
	bin := buildFakeBackend(t)
	sess, _, err := Launch(LaunchConfig{Binary: bin, ReadyTimeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const n = 50
	var wg sync.WaitGroup
	var ok int64
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("req-%d", i)
			msgs, err := sess.Call(ctx, id, "chat.completions", map[string]any{
				"messages": []any{map[string]any{"role": "user", "content": "n"}},
			})
			if err != nil {
				return
			}
			terminal := ""
			for m := range msgs {
				if m.Type == "done" || m.Type == "error" {
					terminal = m.Type
				}
			}
			if terminal == "done" {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if ok != n {
		t.Fatalf("%d/%d concurrent calls completed", ok, n)
	}
}
