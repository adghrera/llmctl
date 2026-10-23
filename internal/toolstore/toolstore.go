// Package toolstore manages user-installed agent tools.
//
// A tool lives at $LLMCTL_HOME/tools/<id>/ and is described by
// llmctl-tool.json:
//
//	{
//	  "name": "word-count",            // display name
//	  "description": "Count words in text",
//	  "entry": "main.py",              // file inside the dir
//	  "runtime": "python",             // python | command
//	  "sandbox": true,                 // default: run in llm-sandbox
//	  "params": {"text": {"type":"string","description":"text to count","required":true}}
//	}
//
// Python entries define  def run(args: dict) -> str | dict
// and receive arguments as JSON on stdin via the runner wrapper.
// "command" runtime (host-only) receives args JSON on stdin, prints
// result text on stdout.
package toolstore

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"llmctl/internal/paths"
)

// ManifestName is the file every tool package must contain.
const ManifestName = "llmctl-tool.json"

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type Param struct {
	Type        string `json:"type,omitempty"` // string|number|boolean
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type Manifest struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Entry       string            `json:"entry"`
	Runtime     string            `json:"runtime,omitempty"` // python (default) | command
	Sandbox     *bool             `json:"sandbox,omitempty"` // nil = default true
	Params      map[string]Param  `json:"params,omitempty"`
}

type Record struct {
	ID          string    `json:"id"`
	Dir         string    `json:"dir"`
	Manifest    Manifest  `json:"manifest"`
	Enabled     bool      `json:"enabled"`
	Sandbox     bool      `json:"sandbox"`     // effective (manifest default)
	Source      string    `json:"source"`      // url or local path it came from
	InstalledAt time.Time `json:"installedAt"`
}

// SandboxEnabled returns the effective sandbox flag.
func (r *Record) SandboxEnabled() bool { return r.Sandbox }

type Store struct {
	dir    string // $LLMCTL_HOME/tools
	enable func(id string, on bool) error
}

func Open() (*Store, error) {
	dir := filepath.Join(paths.Home(), "tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("tools dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Dir(id string) string { return filepath.Join(s.dir, id) }

func genID(name string) string {
	// lowercase FIRST: Go's [a-z0-9_-] is ASCII-only, and stripping before
	// lowercasing would delete every capital letter instead of mapping it.
	base := strings.ToLower(regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(strings.ToLower(name), "-"))
	base = strings.Trim(base, "-")
	if base == "" || len(base) > 40 {
		if len(base) > 40 {
			base = base[:40]
		} else {
			base = "tool"
		}
	}
	b := make([]byte, 3)
	rand.Read(b)
	return base + "-" + hex.EncodeToString(b)
}

// load reads one tool dir.
func (s *Store) load(id string) (*Record, error) {
	dir := s.Dir(id)
	mb, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("tool %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("tool %s: bad manifest: %w", id, err)
	}
	if m.Entry == "" {
		return nil, fmt.Errorf("tool %s: manifest missing entry", id)
	}
	if m.Runtime == "" {
		m.Runtime = "python"
	}
	if m.Runtime != "python" && m.Runtime != "command" {
		return nil, fmt.Errorf("tool %s: unsupported runtime %q (python|command)", id, m.Runtime)
	}
	// entry must stay inside the tool dir
	clean := filepath.Clean(filepath.Join(dir, m.Entry))
	if !strings.HasPrefix(clean, filepath.Clean(dir)+string(os.PathSeparator)) {
		return nil, fmt.Errorf("tool %s: entry escapes tool directory", id)
	}
	if _, err := os.Stat(clean); err != nil {
		return nil, fmt.Errorf("tool %s: entry %s missing", id, m.Entry)
	}
	sandbox := true
	if m.Sandbox != nil {
		sandbox = *m.Sandbox
	}
	if m.Runtime == "command" {
		sandbox = false // command runtime is host-only by definition
	}
	// enabled state: sidecar file (absent = enabled by default on install)
	enabled := true
	if _, err := os.Stat(filepath.Join(dir, ".disabled")); err == nil {
		enabled = false
	}
	// sandbox override sidecar
	if _, err := os.Stat(filepath.Join(dir, ".sandbox-off")); err == nil {
		sandbox = false
	}
	if _, err := os.Stat(filepath.Join(dir, ".sandbox-on")); err == nil {
		sandbox = true
	}
	src, _ := os.ReadFile(filepath.Join(dir, ".source"))
	installed := time.Time{}
	if fi, err := os.Stat(filepath.Join(dir, ManifestName)); err == nil {
		installed = fi.ModTime()
	}
	return &Record{ID: id, Dir: dir, Manifest: m, Enabled: enabled, Sandbox: sandbox,
		Source: string(src), InstalledAt: installed}, nil
}

// List returns all installed tools sorted by name.
func (s *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := []Record{}
	for _, e := range entries {
		if !e.IsDir() || !safeID.MatchString(e.Name()) {
			continue
		}
		r, err := s.load(e.Name())
		if err != nil {
			continue // broken tool: skip in listings
		}
		out = append(out, *r)
	}
	return out, nil
}

func (s *Store) Get(id string) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid tool id")
	}
	return s.load(id)
}

// installDir copies a validated tool package (must contain ManifestName)
// into the registry and returns the record.
func (s *Store) installDir(srcDir, source string, id string) (*Record, error) {
	mb, err := os.ReadFile(filepath.Join(srcDir, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("package has no %s", ManifestName)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("bad manifest: %w", err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return nil, fmt.Errorf("manifest missing name")
	}
	if m.Entry == "" {
		return nil, fmt.Errorf("manifest missing entry")
	}
	if id == "" || !safeID.MatchString(id) {
		id = genID(m.Name)
	}
	final := s.Dir(id)
	if _, err := os.Stat(final); err == nil {
		return nil, fmt.Errorf("tool %s already installed (uninstall first to replace)", id)
	}
	tmp := final + ".tmp"
	os.RemoveAll(tmp)
	if err := copyTree(srcDir, tmp); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	os.WriteFile(filepath.Join(final, ".source"), []byte(source), 0o644)
	rec, err := s.load(id)
	if err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	return rec, nil
}

// InstallFromLocalDir installs a tool from a directory on disk.
func (s *Store) InstallFromLocalDir(srcDir, id string) (*Record, error) {
	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", srcDir)
	}
	return s.installDir(abs, "local:"+abs, id)
}

// InstallFromURL downloads a .zip package and installs it.
func (s *Store) InstallFromURL(url, id string) (*Record, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("url must be http(s)")
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20)) // 32 MiB cap
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("package is not a valid zip: %w", err)
	}
	tmp, err := os.MkdirTemp("", "llmctl-tool-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := extractZipSafe(zr, tmp); err != nil {
		return nil, err
	}
	// allow one top-level directory wrapping the package
	src := tmp
	if _, err := os.Stat(filepath.Join(tmp, ManifestName)); err != nil {
		entries, _ := os.ReadDir(tmp)
		found := ""
		for _, e := range entries {
			if e.IsDir() {
				if _, err := os.Stat(filepath.Join(tmp, e.Name(), ManifestName)); err == nil {
					found = filepath.Join(tmp, e.Name())
					break
				}
			}
		}
		if found == "" {
			return nil, fmt.Errorf("no %s found in package", ManifestName)
		}
		src = found
	}
	return s.installDir(src, url, id)
}

// extractZipSafe unpacks with zip-slip protection and size caps.
func extractZipSafe(zr *zip.Reader, dest string) error {
	var total uint64
	for _, f := range zr.File {
		total += f.UncompressedSize64
		if total > 64<<20 {
			return fmt.Errorf("package too large (uncompressed > 64 MiB)")
		}
	}
	for _, f := range zr.File {
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("zip entry escapes target: %s", f.Name)
		}
		target := filepath.Join(dest, name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) && target != filepath.Clean(dest) {
			return fmt.Errorf("zip entry escapes target: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		os.MkdirAll(filepath.Dir(target), 0o755)
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 32<<20))
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// SetEnabled toggles a tool.
func (s *Store) SetEnabled(id string, on bool) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	flag := filepath.Join(rec.Dir, ".disabled")
	if on {
		return os.Remove(flag)
	}
	return os.WriteFile(flag, []byte("disabled"), 0o644)
}

// SetSandbox toggles sandboxed execution for a tool.
func (s *Store) SetSandbox(id string, on bool) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	onF := filepath.Join(rec.Dir, ".sandbox-on")
	offF := filepath.Join(rec.Dir, ".sandbox-off")
	if on {
		os.Remove(offF)
		return os.WriteFile(onF, []byte("on"), 0o644)
	}
	os.Remove(onF)
	return os.WriteFile(offF, []byte("off"), 0o644)
}

// Delete removes the tool directory.
func (s *Store) Delete(id string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(rec.Dir)
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if fi.Size() > 16<<20 {
			return fmt.Errorf("file too large: %s", rel)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
