// Package rag manages knowledge bases and documents for RAG (Retrieval-
// Augmented Generation). Knowledge bases live at
// $LLMCTL_HOME/knowledge/<kb_id>/ with a kb.json manifest and documents
// stored under docs/<doc_id>/.
package rag

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"llmctl/internal/paths"
)

const (
	ManifestName = "kb.json"
	DocsDir      = "docs"
	MaxDocSize   = 10 << 20 // 10 MiB
)

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Doc is one document inside a knowledge base.
type Doc struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Source      string    `json:"source"` // "upload"
	CreatedAt   time.Time `json:"created_at"`
	Content     string    `json:"content"` // extracted text content
}

// KB is one knowledge base.
type KB struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	DocCount    int       `json:"doc_count"`
}

// Record is a KB with its on-disk directory.
type Record struct {
	ID   string `json:"id"`
	Dir  string `json:"dir"`
	KB   KB     `json:"kb"`
	Docs []Doc  `json:"docs"`
}

// Store manages knowledge bases on disk.
type Store struct {
	dir string // $LLMCTL_HOME/knowledge
}

// Open opens (creating if needed) the rag store.
func Open() (*Store, error) {
	dir := filepath.Join(paths.Home(), "knowledge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("knowledge dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the on-disk directory for a KB id.
func (s *Store) Dir(id string) string { return filepath.Join(s.dir, id) }

// genID generates a random KB id.
func genID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "kb-000000"
	}
	return "kb-" + hex.EncodeToString(b)
}

// load reads one KB dir.
func (s *Store) load(id string) (*Record, error) {
	dir := s.Dir(id)
	mb, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("kb %s: %w", id, err)
	}
	var kb KB
	if err := json.Unmarshal(mb, &kb); err != nil {
		return nil, fmt.Errorf("kb %s: bad manifest: %w", id, err)
	}
	if kb.ID == "" {
		kb.ID = id
	}
	// Load docs.
	docsDir := filepath.Join(dir, DocsDir)
	docs, err := s.loadDocs(docsDir)
	if err != nil {
		return nil, err
	}
	kb.DocCount = len(docs)
	return &Record{
		ID:   kb.ID,
		Dir:  dir,
		KB:   kb,
		Docs: docs,
	}, nil
}

func (s *Store) loadDocs(docsDir string) ([]Doc, error) {
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []Doc{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		docDir := filepath.Join(docsDir, e.Name())
		doc, err := s.loadDoc(docDir)
		if err != nil {
			continue
		}
		out = append(out, *doc)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *Store) loadDoc(dir string) (*Doc, error) {
	mb, err := os.ReadFile(filepath.Join(dir, "doc.json"))
	if err != nil {
		return nil, err
	}
	var d Doc
	if err := json.Unmarshal(mb, &d); err != nil {
		return nil, err
	}
	// Read content.
	contentPath := filepath.Join(dir, "content.txt")
	content, err := os.ReadFile(contentPath)
	if err != nil {
		d.Content = ""
	} else {
		d.Content = string(content)
	}
	return &d, nil
}

// List returns all knowledge bases sorted by updated_at descending.
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
	sort.Slice(out, func(i, j int) bool {
		return out[i].KB.UpdatedAt.After(out[j].KB.UpdatedAt)
	})
	return out, nil
}

// Get returns one KB by id.
func (s *Store) Get(id string) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid kb id")
	}
	return s.load(id)
}

// Create writes a new KB directory and returns the record.
func (s *Store) Create(name, description string) (*Record, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("kb missing name")
	}
	id := genID()
	final := s.Dir(id)
	if _, err := os.Stat(final); err == nil {
		return nil, fmt.Errorf("kb %s already exists", id)
	}
	if err := os.MkdirAll(final, 0o755); err != nil {
		return nil, err
	}
	docsDir := filepath.Join(final, DocsDir)
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	now := time.Now().UTC()
	kb := KB{
		ID:          id,
		Name:        name,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := writeJSON(filepath.Join(final, ManifestName), kb); err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	return &Record{
		ID:   id,
		Dir:  final,
		KB:   kb,
		Docs: []Doc{},
	}, nil
}

// Update replaces an existing KB's metadata.
func (s *Store) Update(id, name, description string) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid kb id")
	}
	rec, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("kb missing name")
	}
	rec.KB.Name = name
	rec.KB.Description = description
	rec.KB.UpdatedAt = time.Now().UTC()
	if err := writeJSON(filepath.Join(rec.Dir, ManifestName), rec.KB); err != nil {
		return nil, err
	}
	return s.load(id)
}

// Delete removes the KB directory and all documents.
func (s *Store) Delete(id string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(rec.Dir)
}

// AddDoc adds a document to a KB.
func (s *Store) AddDoc(kbID, filename, contentType string, content []byte) (*Doc, error) {
	if len(content) > MaxDocSize {
		return nil, fmt.Errorf("document too large (max %d MiB)", MaxDocSize/1024/1024)
	}
	rec, err := s.Get(kbID)
	if err != nil {
		return nil, err
	}
	docID := genID()
	docDir := filepath.Join(rec.Dir, DocsDir, docID)
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	doc := Doc{
		ID:          docID,
		Filename:    filename,
		ContentType: contentType,
		Size:        int64(len(content)),
		Source:      "upload",
		CreatedAt:   now,
		Content:     string(content),
	}
	if err := writeJSON(filepath.Join(docDir, "doc.json"), doc); err != nil {
		os.RemoveAll(docDir)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(docDir, "content.txt"), content, 0o644); err != nil {
		os.RemoveAll(docDir)
		return nil, err
	}
	// Update doc count in KB.
	rec.KB.DocCount++
	rec.KB.UpdatedAt = now
	if err := writeJSON(filepath.Join(rec.Dir, ManifestName), rec.KB); err != nil {
		return nil, err
	}
	return &doc, nil
}

// DeleteDoc removes a document from a KB.
func (s *Store) DeleteDoc(kbID, docID string) error {
	rec, err := s.Get(kbID)
	if err != nil {
		return err
	}
	docDir := filepath.Join(rec.Dir, DocsDir, docID)
	if err := os.RemoveAll(docDir); err != nil {
		return err
	}
	// Update doc count.
	rec.KB.DocCount--
	if rec.KB.DocCount < 0 {
		rec.KB.DocCount = 0
	}
	rec.KB.UpdatedAt = time.Now().UTC()
	if err := writeJSON(filepath.Join(rec.Dir, ManifestName), rec.KB); err != nil {
		return err
	}
	return nil
}

// GetDoc returns a document by kb id and doc id.
func (s *Store) GetDoc(kbID, docID string) (*Doc, error) {
	rec, err := s.Get(kbID)
	if err != nil {
		return nil, err
	}
	docDir := filepath.Join(rec.Dir, DocsDir, docID)
	doc, err := s.loadDoc(docDir)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// Search searches KB documents for a query (simple keyword match).
func (s *Store) Search(kbID, query string, limit int) ([]Doc, error) {
	rec, err := s.Get(kbID)
	if err != nil {
		return nil, err
	}
	qLower := strings.ToLower(query)
	var results []Doc
	for _, d := range rec.Docs {
		if strings.Contains(strings.ToLower(d.Content), qLower) ||
			strings.Contains(strings.ToLower(d.Filename), qLower) {
			results = append(results, d)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

// writeJSON writes a value as JSON to a file.
func writeJSON(path string, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Ensure fs is used.
var _ = fs.ReadDir
