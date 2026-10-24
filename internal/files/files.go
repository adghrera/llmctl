// Package files manages user-uploaded files stored under
// $LLMCTL_HOME/files/<id>/. Only text-based files are supported for
// content injection (PDF/DOCX require external libraries).
package files

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"llmctl/internal/paths"
)

const MaxFileSize = 10 << 20 // 10 MiB

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Entry is one uploaded file.
type Entry struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	Source      string    `json:"source"` // "upload"
	CreatedAt   time.Time `json:"created_at"`
}

// Record is an entry with its on-disk directory.
type Record struct {
	ID   string `json:"id"`
	Dir  string `json:"dir"`
	Entry Entry `json:"entry"`
}

// Store manages uploaded files on disk.
type Store struct {
	dir string // $LLMCTL_HOME/files
}

// Open opens (creating if needed) the files store.
func Open() (*Store, error) {
	dir := filepath.Join(paths.Home(), "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("files dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the on-disk directory for a file id.
func (s *Store) Dir(id string) string { return filepath.Join(s.dir, id) }

// genID generates a random file id.
func genID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "file-000000"
	}
	return "file-" + hex.EncodeToString(b)
}

// load reads one file dir.
func (s *Store) load(id string) (*Record, error) {
	dir := s.Dir(id)
	mb, err := os.ReadFile(filepath.Join(dir, "entry.json"))
	if err != nil {
		return nil, fmt.Errorf("file %s: %w", id, err)
	}
	var e Entry
	if err := json.Unmarshal(mb, &e); err != nil {
		return nil, fmt.Errorf("file %s: bad entry: %w", id, err)
	}
	if e.ID == "" {
		e.ID = id
	}
	return &Record{
		ID:   e.ID,
		Dir:  dir,
		Entry: e,
	}, nil
}

// List returns all uploaded files sorted by created_at descending.
func (s *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []Record{}
	for _, e := range entries {
		if !e.IsDir() || !safeID.MatchString(e.Name()) {
			continue
		}
		r, err := s.load(e.Name())
		if err != nil {
			continue
		}
		out = append(out, *r)
	}
	// Sort by created_at descending.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i].Entry.CreatedAt.Before(out[j].Entry.CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// Get returns one file by id.
func (s *Store) Get(id string) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid file id")
	}
	return s.load(id)
}

// ContentPath returns the path to the stored file content.
func (r *Record) ContentPath() string {
	return filepath.Join(r.Dir, "content")
}

// Create stores an uploaded file and returns the record.
func (s *Store) Create(filename, contentType string, content []byte) (*Record, error) {
	if len(content) > MaxFileSize {
		return nil, fmt.Errorf("file too large (max %d MiB)", MaxFileSize/1024/1024)
	}
	id := genID()
	final := s.Dir(id)
	if _, err := os.Stat(final); err == nil {
		return nil, fmt.Errorf("file %s already exists", id)
	}
	if err := os.MkdirAll(final, 0o755); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := Entry{
		ID:          id,
		Filename:    filename,
		ContentType: contentType,
		Size:        int64(len(content)),
		Source:      "upload",
		CreatedAt:   now,
	}
	if err := writeJSON(filepath.Join(final, "entry.json"), e); err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(final, "content"), content, 0o644); err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	return s.load(id)
}

// Delete removes the file directory.
func (s *Store) Delete(id string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(rec.Dir)
}

// ReadContent returns the stored file content.
func (r *Record) ReadContent() ([]byte, error) {
	return os.ReadFile(filepath.Join(r.Dir, "content"))
}

// IsText reports whether the file is likely text-based.
func (r *Record) IsText() bool {
	ct := strings.ToLower(r.Entry.ContentType)
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	if ct == "application/json" || ct == "application/javascript" || ct == "application/xml" {
		return true
	}
	// Fall back to extension check.
	ext := strings.ToLower(filepath.Ext(r.Entry.Filename))
	textExts := map[string]bool{
		".txt": true, ".md": true, ".py": true, ".js": true, ".ts": true,
		".json": true, ".xml": true, ".html": true, ".css": true,
		".go": true, ".rs": true, ".java": true, ".c": true, ".h": true,
		".yml": true, ".yaml": true, ".toml": true, ".ini": true,
		".cfg": true, ".conf": true, ".log": true,
	}
	return textExts[ext]
}

// writeJSON writes a value as JSON to a file.
func writeJSON(path string, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
