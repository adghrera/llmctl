package backend

import (
	"os"
	"path/filepath"
	"testing"

	"llmctl/internal/store"
)

// TestFindBackendBinary verifies a bare binary name resolves to the backend's
// install dir (both url/files layout and github-release versioned layout).
func TestFindBackendBinary(t *testing.T) {
	t.Setenv("LLMCTL_HOME", t.TempDir())

	// url/files layout: <dir>/<name>
	urlDir := filepath.Join(os.Getenv("LLMCTL_HOME"), "backends", "urlbe")
	os.MkdirAll(urlDir, 0o755)
	os.WriteFile(filepath.Join(urlDir, "my-server"), []byte("x"), 0o755)

	// github-release layout: <dir>/<version>/<name>
	ghDir := filepath.Join(os.Getenv("LLMCTL_HOME"), "backends", "ghbe", "v1")
	os.MkdirAll(ghDir, 0o755)
	os.WriteFile(filepath.Join(ghDir, "my-server"), []byte("x"), 0o755)

	// nested github-release layout: <dir>/<version>/<archive-dir>/bin/<name>
	nestedDir := filepath.Join(os.Getenv("LLMCTL_HOME"), "backends", "nestedbe", "b11850", "llama-b11850", "bin")
	os.MkdirAll(nestedDir, 0o755)
	os.WriteFile(filepath.Join(nestedDir, "llama-server"), []byte("x"), 0o755)

	got, err := findBackendBinary("urlbe", "my-server")
	if err != nil {
		t.Fatalf("urlbe: %v", err)
	}
	if want := filepath.Join(urlDir, "my-server"); got != want {
		t.Fatalf("urlbe = %q, want %q", got, want)
	}

	got, err = findBackendBinary("ghbe", "my-server")
	if err != nil {
		t.Fatalf("ghbe: %v", err)
	}
	if want := filepath.Join(ghDir, "my-server"); got != want {
		t.Fatalf("ghbe = %q, want %q", got, want)
	}

	got, err = findBackendBinary("nestedbe", "llama-server")
	if err != nil {
		t.Fatalf("nestedbe: %v", err)
	}
	if want := filepath.Join(nestedDir, "llama-server"); got != want {
		t.Fatalf("nestedbe = %q, want %q", got, want)
	}

	// Missing binary -> error.
	if _, err := findBackendBinary("nope", "my-server"); err == nil {
		t.Fatal("expected error for missing backend")
	}
}

// TestPluginSpecResolvesBinary verifies a plugin's bare binary name is
// resolved to an absolute path in Spec (so exec.Command finds it).
func TestPluginSpecResolvesBinary(t *testing.T) {
	t.Setenv("LLMCTL_HOME", t.TempDir())
	dir := filepath.Join(os.Getenv("LLMCTL_HOME"), "backends", "mybe")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "my-server"), []byte("x"), 0o755)

	m := &PluginMeta{
		ID: "mybe", Transport: TransportHTTP, Kind: KindProcess,
		Binary: "my-server",
		Args:   []string{"--port", "{port}", "--model", "{model}"},
	}
	b, err := newPluginBackend(m)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := b.Spec(&store.ModelRecord{Path: "/models/qwen.gguf"}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(spec.Binary) {
		t.Fatalf("binary not resolved to absolute path: %q", spec.Binary)
	}
	if want := filepath.Join(dir, "my-server"); spec.Binary != want {
		t.Fatalf("binary = %q, want %q", spec.Binary, want)
	}
}

// TestPluginSpecBareBinaryFallsBackToPath verifies a binary NOT in the backend
// dir (e.g. "python3") stays bare so the OS PATH lookup still works.
func TestPluginSpecBareBinaryFallsBackToPath(t *testing.T) {
	t.Setenv("LLMCTL_HOME", t.TempDir())
	m := &PluginMeta{
		ID: "pybe", Transport: TransportStdio, Kind: KindProcess,
		Binary: "python3",
		Args:   []string{"{backend}/script.py", "--model", "{model}"},
	}
	b, err := newPluginBackend(m)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := b.Spec(&store.ModelRecord{Path: "/models/qwen.gguf"}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Binary != "python3" {
		t.Fatalf("expected bare 'python3' (PATH lookup), got %q", spec.Binary)
	}
}
