// Package memory manages persistent long-term memory entries for the agent.
// Each entry is a JSON file under $LLMCTL_HOME/memory/<id>/ containing text
// content, tags, and metadata. Entries can be searched by keyword/substring
// and injected as context during agent chats.
package memory

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"llmctl/internal/paths"
)

// EntryName is the file every memory entry directory must contain.
const EntryName = "entry.json"

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Entry is one memory item.
type Entry struct {
	ID          string    `json:"id"`
	Content     string    `json:"content"`
	Tags        []string  `json:"tags,omitempty"`
	Source      string    `json:"source,omitempty"`      // conversation id or "manual"
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Record is an entry with its on-disk directory.
type Record struct {
	ID        string    `json:"id"`
	Dir       string    `json:"dir"`
	Entry     Entry     `json:"entry"`
	Source    string    `json:"source"`
}

// Store manages memory entries on disk.
type Store struct {
	dir string // $LLMCTL_HOME/memory
}

// Open opens (creating if needed) the memory store.
func Open() (*Store, error) {
	dir := filepath.Join(paths.Home(), "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("memory dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the on-disk directory for an entry id.
func (s *Store) Dir(id string) string { return filepath.Join(s.dir, id) }

// genID generates a random memory entry id.
func genID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "mem-000000"
	}
	return "mem-" + hex.EncodeToString(b)
}

// load reads one entry dir.
func (s *Store) load(id string) (*Record, error) {
	dir := s.Dir(id)
	mb, err := os.ReadFile(filepath.Join(dir, EntryName))
	if err != nil {
		return nil, fmt.Errorf("memory %s: %w", id, err)
	}
	var e Entry
	if err := json.Unmarshal(mb, &e); err != nil {
		return nil, fmt.Errorf("memory %s: bad entry: %w", id, err)
	}
	if e.ID == "" {
		e.ID = id
	}
	if e.Content == "" {
		return nil, fmt.Errorf("memory %s: entry missing content", id)
	}
	return &Record{
		ID:     e.ID,
		Dir:    dir,
		Entry:  e,
		Source: e.Source,
	}, nil
}

// List returns all memory entries sorted by updated_at descending.
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
			continue // broken entry: skip in listings
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Entry.UpdatedAt.After(out[j].Entry.UpdatedAt)
	})
	return out, nil
}

// Get returns one entry by id.
func (s *Store) Get(id string) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid memory id")
	}
	return s.load(id)
}

// Search finds entries whose content or tags contain q (case-insensitive).
// Returns at most limit results.
func (s *Store) Search(q string, limit int) ([]Record, error) {
	if limit <= 0 {
		limit = 20
	}
	qLower := strings.ToLower(q)
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var results []Record
	for _, e := range entries {
		if !e.IsDir() || !safeID.MatchString(e.Name()) {
			continue
		}
		r, err := s.load(e.Name())
		if err != nil {
			continue
		}
		contentMatch := strings.Contains(strings.ToLower(r.Entry.Content), qLower)
		tagMatch := false
		for _, t := range r.Entry.Tags {
			if strings.Contains(strings.ToLower(t), qLower) {
				tagMatch = true
				break
			}
		}
		if contentMatch || tagMatch {
			results = append(results, *r)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Entry.UpdatedAt.After(results[j].Entry.UpdatedAt)
	})
	return results, nil
}

// Create writes a new memory entry directory and returns the record.
func (s *Store) Create(content string, tags []string, source string) (*Record, error) {
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("memory entry missing content")
	}
	id := genID()
	final := s.Dir(id)
	if _, err := os.Stat(final); err == nil {
		return nil, fmt.Errorf("memory %s already exists", id)
	}
	if err := os.MkdirAll(final, 0o755); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := Entry{
		ID:        id,
		Content:   content,
		Tags:      tags,
		Source:    source,
		CreatedAt: now,
		UpdatedAt: now,
	}
	eb, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(final, EntryName), eb, 0o644); err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	return s.load(id)
}

// Update replaces an existing entry's content/tags/source.
func (s *Store) Update(id string, content string, tags []string, source string) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid memory id")
	}
	rec, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("memory entry missing content")
	}
	rec.Entry.Content = content
	rec.Entry.Tags = tags
	rec.Entry.Source = source
	rec.Entry.UpdatedAt = time.Now().UTC()
	eb, err := json.MarshalIndent(rec.Entry, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(rec.Dir, EntryName), eb, 0o644); err != nil {
		return nil, err
	}
	return s.load(id)
}

// Delete removes the entry directory.
func (s *Store) Delete(id string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(rec.Dir)
}

// FormatForContext formats entries as a compact context block for injection
// into the agent's system prompt.
func FormatForContext(entries []Record) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Relevant memories:\n")
	for i, e := range entries {
		b.WriteString(fmt.Sprintf("%d. %s", i+1, e.Entry.Content))
		if len(e.Entry.Tags) > 0 {
			b.WriteString(" [tags: " + strings.Join(e.Entry.Tags, ", ") + "]")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
