package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateRestoreRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LLMCTL_HOME", home)

	// Seed some state.
	mustWrite(t, filepath.Join(home, "state.json"), `{"backends":{}}`)
	mustWrite(t, filepath.Join(home, "chats", "c1.json"), `{"id":"c1"}`)
	mustWrite(t, filepath.Join(home, "knowledge", "kb1", "kb.json"), `{"id":"kb1"}`)
	mustWrite(t, filepath.Join(home, "memory", "mem.json"), `{"items":[]}`)
	// Excluded content:
	mustWrite(t, filepath.Join(home, "models", "m.gguf"), "GGUFDATA")
	mustWrite(t, filepath.Join(home, "backends", "llama.cpp", "llama-server"), "bin")
	mustWrite(t, filepath.Join(home, "logs", "inst", "stdout.log"), "log")
	mustWrite(t, filepath.Join(home, "llmctl.log"), "daemon log")
	mustWrite(t, filepath.Join(home, "state.json.lock"), "lock")

	var buf bytes.Buffer
	if err := Create(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty archive")
	}

	// Restore into a fresh home.
	home2 := t.TempDir()
	t.Setenv("LLMCTL_HOME", home2)
	n, err := Restore(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if n < 4 {
		t.Fatalf("restored %d files, want >= 4", n)
	}
	for _, rel := range []string{"state.json", "chats/c1.json", "knowledge/kb1/kb.json", "memory/mem.json"} {
		if _, err := os.Stat(filepath.Join(home2, rel)); err != nil {
			t.Errorf("missing after restore: %s", rel)
		}
	}
	for _, rel := range []string{"models/m.gguf", "backends/llama.cpp/llama-server", "logs/inst/stdout.log", "llmctl.log", "state.json.lock"} {
		if _, err := os.Stat(filepath.Join(home2, rel)); err == nil {
			t.Errorf("should NOT be restored: %s", rel)
		}
	}
}

func TestRestoreRejectsTraversal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LLMCTL_HOME", home)
	// Build a malicious archive in memory.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "../escaped.txt", Mode: 0o644, Size: 5, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("pwned")); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()

	if _, err := Restore(&buf); err == nil {
		t.Error("expected error for traversal path")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(home), "escaped.txt")); err == nil {
		t.Error("traversal file was written")
	}
}

func TestRestoreNotGzip(t *testing.T) {
	t.Setenv("LLMCTL_HOME", t.TempDir())
	if _, err := Restore(bytes.NewReader([]byte("not gzip"))); err == nil {
		t.Error("expected error for non-gzip input")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
