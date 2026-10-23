package toolexec

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"llmctl/internal/toolstore"
)

func hasPython() bool {
	for _, n := range []string{"python", "python3", "py"} {
		if _, err := exec.LookPath(n); err == nil {
			return true
		}
	}
	return false
}

func mkRecord(t *testing.T, entry string, runtime string, sandbox bool) *toolstore.Record {
	t.Helper()
	dir := t.TempDir()
	man := toolstore.Manifest{Name: "t", Entry: entry, Runtime: runtime, Sandbox: &sandbox}
	mb, _ := json.Marshal(man)
	os.WriteFile(filepath.Join(dir, toolstore.ManifestName), mb, 0o644)
	return &toolstore.Record{ID: "t", Dir: dir, Manifest: man, Enabled: true, Sandbox: sandbox}
}

func TestRunHostPython(t *testing.T) {
	if !hasPython() {
		t.Skip("no python")
	}
	rec := mkRecord(t, "main.py", "python", false)
	os.WriteFile(filepath.Join(rec.Dir, "main.py"),
		[]byte("def run(args):\n    return 'got:' + args['x']\n"), 0o644)
	res := Run(context.Background(), rec, `{"x":"hi"}`, 30*time.Second)
	if !res.OK {
		t.Fatalf("not ok: %+v", res)
	}
	if res.Result != "got:hi" {
		t.Fatalf("result = %q", res.Result)
	}
	if res.Sandbox {
		t.Fatal("expected host run")
	}
}

func TestRunHostPythonDictResult(t *testing.T) {
	if !hasPython() {
		t.Skip("no python")
	}
	rec := mkRecord(t, "main.py", "python", false)
	os.WriteFile(filepath.Join(rec.Dir, "main.py"),
		[]byte("def run(args):\n    return {\"n\": args[\"a\"] + args[\"b\"]}\n"), 0o644)
	res := Run(context.Background(), rec, `{"a":2,"b":3}`, 30*time.Second)
	if !res.OK || res.Result != `{"n": 5}` {
		t.Fatalf("res = %+v", res)
	}
}

func TestRunHostPythonError(t *testing.T) {
	if !hasPython() {
		t.Skip("no python")
	}
	rec := mkRecord(t, "main.py", "python", false)
	os.WriteFile(filepath.Join(rec.Dir, "main.py"),
		[]byte("def run(args):\n    raise ValueError('boom')\n"), 0o644)
	res := Run(context.Background(), rec, `{}`, 30*time.Second)
	if res.OK {
		t.Fatal("expected failure")
	}
	if res.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestRunHostTimeout(t *testing.T) {
	if !hasPython() {
		t.Skip("no python")
	}
	rec := mkRecord(t, "main.py", "python", false)
	os.WriteFile(filepath.Join(rec.Dir, "main.py"),
		[]byte("import time\ndef run(args):\n    time.sleep(30)\n    return 'late'\n"), 0o644)
	res := Run(context.Background(), rec, `{}`, 2*time.Second)
	if res.OK {
		t.Fatal("expected timeout")
	}
}

func TestRunSandboxUnsupportedRuntime(t *testing.T) {
	rec := mkRecord(t, "prog.exe", "command", false)
	rec.Sandbox = true // force sandbox on for a command runtime
	res := Run(context.Background(), rec, `{}`, 5*time.Second)
	if res.OK || res.Error == "" {
		t.Fatalf("expected rejection, got %+v", res)
	}
}
