package toolstore

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// point the registry at a temp dir
func openTemp(t *testing.T) *Store {
	t.Helper()
	t.Setenv("LLMCTL_HOME", t.TempDir())
	st, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func writeToolDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	man := Manifest{Name: "Word Counter", Description: "count words", Entry: "main.py",
		Params: map[string]Param{"text": {Type: "string", Required: true}}}
	mb, _ := json.Marshal(man)
	os.WriteFile(filepath.Join(dir, ManifestName), mb, 0o644)
	os.WriteFile(filepath.Join(dir, "main.py"),
		[]byte("def run(args):\n    return str(len(args.get('text','').split()))\n"), 0o644)
	return dir
}

func TestInstallLocalAndLifecycle(t *testing.T) {
	st := openTemp(t)
	src := writeToolDir(t)

	rec, err := st.InstallFromLocalDir(src, "")
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Enabled || !rec.Sandbox {
		t.Fatalf("defaults: enabled=%v sandbox=%v, want true/true", rec.Enabled, rec.Sandbox)
	}

	list, _ := st.List()
	if len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}

	// disable / enable
	if err := st.SetEnabled(rec.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(rec.ID)
	if got.Enabled {
		t.Fatal("expected disabled")
	}
	st.SetEnabled(rec.ID, true)
	got, _ = st.Get(rec.ID)
	if !got.Enabled {
		t.Fatal("expected enabled")
	}

	// sandbox toggle
	if err := st.SetSandbox(rec.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get(rec.ID)
	if got.SandboxEnabled() {
		t.Fatal("expected sandbox off")
	}

	// uninstall
	if err := st.Delete(rec.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = st.List()
	if len(list) != 0 {
		t.Fatalf("expected empty after delete, got %d", len(list))
	}
}

func TestInstallDuplicateRejected(t *testing.T) {
	st := openTemp(t)
	src := writeToolDir(t)
	rec, _ := st.InstallFromLocalDir(src, "fixed-id")
	if rec.ID != "fixed-id" {
		t.Fatalf("id = %s", rec.ID)
	}
	if _, err := st.InstallFromLocalDir(src, "fixed-id"); err == nil {
		t.Fatal("expected duplicate install to fail")
	}
}

func TestInstallFromURLZip(t *testing.T) {
	st := openTemp(t)
	// build a zip with one wrapping dir
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	man, _ := json.Marshal(Manifest{Name: "Zip Tool", Entry: "main.py"})
	f, _ := zw.Create("pkg/" + ManifestName)
	f.Write(man)
	f, _ = zw.Create("pkg/main.py")
	f.Write([]byte("def run(args):\n    return 'hi'\n"))
	zw.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	defer srv.Close()

	rec, err := st.InstallFromURL(srv.URL+"/tool.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Manifest.Name != "Zip Tool" {
		t.Fatalf("name = %s", rec.Manifest.Name)
	}
}

func TestInstallZipSlipRejected(t *testing.T) {
	st := openTemp(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("evil/../../../evil.txt")
	f.Write([]byte("nope"))
	man, _ := json.Marshal(Manifest{Name: "Evil", Entry: "evil.txt"})
	f2, _ := zw.Create(ManifestName)
	f2.Write(man)
	zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	defer srv.Close()
	if _, err := st.InstallFromURL(srv.URL, ""); err == nil {
		t.Fatal("expected zip-slip rejection")
	}
}

func TestInstallMissingManifest(t *testing.T) {
	st := openTemp(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.py"), []byte("def run(a): return ''"), 0o644)
	if _, err := st.InstallFromLocalDir(dir, ""); err == nil {
		t.Fatal("expected manifest error")
	}
}

func TestEntryEscapeRejected(t *testing.T) {
	st := openTemp(t)
	dir := t.TempDir()
	man, _ := json.Marshal(Manifest{Name: "Escape", Entry: "../../etc/passwd"})
	os.WriteFile(filepath.Join(dir, ManifestName), man, 0o644)
	rec, err := st.InstallFromLocalDir(dir, "")
	if err == nil && rec != nil {
		t.Fatal("expected escape rejection")
	}
}
