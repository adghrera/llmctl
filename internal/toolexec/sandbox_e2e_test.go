package toolexec

// Opt-in sandbox E2E: LLMCTL_TEST_SANDBOX=1 go test -run TestRunSandboxed -v
// Requires llm-sandbox + a running podman/docker daemon; first run pulls
// the sandbox image.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"llmctl/internal/toolstore"
)

func TestRunSandboxed(t *testing.T) {
	if os.Getenv("LLMCTL_TEST_SANDBOX") != "1" {
		t.Skip("set LLMCTL_TEST_SANDBOX=1 to run (needs llm-sandbox + podman/docker)")
	}
	dir := t.TempDir()
	man := toolstore.Manifest{Name: "probe", Entry: "main.py", Runtime: "python"}
	mb, _ := json.Marshal(man)
	os.WriteFile(filepath.Join(dir, toolstore.ManifestName), mb, 0o644)
	// verifies: code executes in sandbox AND network is off
	os.WriteFile(filepath.Join(dir, "main.py"), []byte(
		"import socket, json, sys\n"+
			"def run(args):\n"+
			"    net = 'YES'\n"+
			"    try:\n"+
			"        socket.create_connection(('1.1.1.1', 80), timeout=3)\n"+
			"    except Exception:\n"+
			"        net = 'NO'\n"+
			"    return json.dumps({'echo': args.get('x', ''), 'network': net})\n"), 0o644)
	sb := true
	rec := &toolstore.Record{ID: "probe", Dir: dir, Manifest: man, Enabled: true, Sandbox: sb}

	res := Run(context.Background(), rec, `{"x":"ping"}`, 300*time.Second)
	t.Logf("sandbox result: %s", mustJSON(res))
	if !res.OK {
		t.Fatalf("sandbox run failed: %s / %s", res.Error, res.Stderr)
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(res.Result), &out); err != nil {
		t.Fatalf("bad result %q: %v", res.Result, err)
	}
	if out["echo"] != "ping" {
		t.Fatalf("echo = %q", out["echo"])
	}
	if out["network"] != "NO" {
		t.Fatalf("SECURITY: sandbox had network access")
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
