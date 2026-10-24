// Package skill manages user-defined prompt templates ("skills").
//
// A skill lives at $LLMCTL_HOME/skills/<id>/ and is described by skill.json:
//
//	{
//	  "id": "code-review-abc123",            // auto-generated from short_name
//	  "short_name": "code-review",            // short slug used in /s.<skill>
//	  "name": "Code Review",                  // display name
//	  "description": "Review code for bugs",  // human-readable description
//	  "prompt_template": "...",               // the prompt template with {{input}} and {{conversation}} placeholders
//	  "tags": ["code", "review"],             // optional tags
//	  "enabled": true,                        // whether the skill can be invoked
//	  "created_at": "...",                    // creation timestamp
//	  "updated_at": "..."                     // last update timestamp
//	}
//
// Skill injection: when a user types "/s.<short_name> <rest>" in chat, the
// skill's rendered prompt ({{input}} = <rest>, {{conversation}} = prior
// history) is inserted as a separate user message immediately before the
// user's actual input, after the prior conversation history.
package skills

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

// ManifestName is the file every skill directory must contain.
const ManifestName = "skill.json"

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var safeShortName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Manifest describes a skill.
type Manifest struct {
	ID            string    `json:"id"`
	ShortName     string    `json:"short_name"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	PromptTemplate string   `json:"prompt_template"`
	Tags          []string  `json:"tags,omitempty"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Record is a skill with its on-disk directory.
type Record struct {
	ID          string    `json:"id"`
	Dir         string    `json:"dir"`
	Manifest    Manifest  `json:"manifest"`
	Enabled     bool      `json:"enabled"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store manages skills on disk.
type Store struct {
	dir string // $LLMCTL_HOME/skills
}

// Open opens (creating if needed) the skills store.
func Open() (*Store, error) {
	dir := filepath.Join(paths.Home(), "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("skills dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the on-disk directory for a skill id.
func (s *Store) Dir(id string) string { return filepath.Join(s.dir, id) }

// genID generates a skill id from the short name: slug + random suffix.
func genID(shortName string) string {
	base := strings.ToLower(regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(shortName, "-"))
	base = strings.Trim(base, "-")
	if base == "" {
		base = "skill"
	}
	if len(base) > 40 {
		base = base[:40]
	}
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return base + "-000000"
	}
	return base + "-" + hex.EncodeToString(b)
}

// load reads one skill dir.
func (s *Store) load(id string) (*Record, error) {
	dir := s.Dir(id)
	mb, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("skill %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("skill %s: bad manifest: %w", id, err)
	}
	if m.ID == "" {
		m.ID = id
	}
	if m.ShortName == "" {
		return nil, fmt.Errorf("skill %s: manifest missing short_name", id)
	}
	if m.PromptTemplate == "" {
		return nil, fmt.Errorf("skill %s: manifest missing prompt_template", id)
	}
	enabled := m.Enabled // default to manifest value
	return &Record{
		ID:        m.ID,
		Dir:       dir,
		Manifest:  m,
		Enabled:   enabled,
		Source:    m.ID,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

// List returns all installed skills sorted by short name.
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
			continue // broken skill: skip in listings
		}
		out = append(out, *r)
	}
	// Sort by short name.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i].Manifest.ShortName > out[j].Manifest.ShortName {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// Get returns one skill by id.
func (s *Store) Get(id string) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid skill id")
	}
	return s.load(id)
}

// GetByShortName returns one skill by short name.
func (s *Store) GetByShortName(shortName string) (*Record, error) {
	if !safeShortName.MatchString(shortName) {
		return nil, fmt.Errorf("invalid skill short name")
	}
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Manifest.ShortName == shortName {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("skill %q not found", shortName)
}

// Create writes a new skill directory and returns the record.
func (s *Store) Create(m Manifest) (*Record, error) {
	if strings.TrimSpace(m.ShortName) == "" {
		return nil, fmt.Errorf("manifest missing short_name")
	}
	if !safeShortName.MatchString(m.ShortName) {
		return nil, fmt.Errorf("invalid short_name %q", m.ShortName)
	}
	if strings.TrimSpace(m.PromptTemplate) == "" {
		return nil, fmt.Errorf("manifest missing prompt_template")
	}
	id := genID(m.ShortName)
	final := s.Dir(id)
	if _, err := os.Stat(final); err == nil {
		return nil, fmt.Errorf("skill %s already exists", id)
	}
	if err := os.MkdirAll(final, 0o755); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	m.ID = id
	m.CreatedAt = now
	m.UpdatedAt = now
	m.Enabled = true // default: skills are enabled on creation
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(final, ManifestName), mb, 0o644); err != nil {
		os.RemoveAll(final)
		return nil, err
	}
	return s.load(id)
}

// Update replaces an existing skill's manifest.
func (s *Store) Update(id string, m Manifest) (*Record, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid skill id")
	}
	rec, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	// Preserve id, created_at, dir; update everything else.
	m.ID = id
	m.CreatedAt = rec.Manifest.CreatedAt
	m.UpdatedAt = time.Now().UTC()
	if strings.TrimSpace(m.ShortName) == "" {
		return nil, fmt.Errorf("manifest missing short_name")
	}
	if !safeShortName.MatchString(m.ShortName) {
		return nil, fmt.Errorf("invalid short_name %q", m.ShortName)
	}
	if strings.TrimSpace(m.PromptTemplate) == "" {
		return nil, fmt.Errorf("manifest missing prompt_template")
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(rec.Dir, ManifestName), mb, 0o644); err != nil {
		return nil, err
	}
	return s.load(id)
}

// SetEnabled toggles a skill's enabled state.
func (s *Store) SetEnabled(id string, on bool) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	m := rec.Manifest
	m.Enabled = on
	m.UpdatedAt = time.Now().UTC()
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(rec.Dir, ManifestName), mb, 0o644)
}

// Delete removes the skill directory.
func (s *Store) Delete(id string) error {
	rec, err := s.Get(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(rec.Dir)
}

// Render renders a skill's prompt template with the given input and
// conversation history. {{input}} is replaced with the slash-command text;
// {{conversation}} is replaced with the formatted prior history.
func (r *Record) Render(input, conversation string) string {
	t := r.Manifest.PromptTemplate
	t = strings.ReplaceAll(t, "{{input}}", input)
	t = strings.ReplaceAll(t, "{{conversation}}", conversation)
	return t
}

// FormatConversation formats prior conversation history for injection.
// It returns the last maxTurns user/assistant turns as a compact string.
func FormatConversation(messages []map[string]string, maxTurns int) string {
	if len(messages) == 0 {
		return "(none)"
	}
	start := 0
	if len(messages) > maxTurns*2 {
		start = len(messages) - maxTurns*2
	}
	var b strings.Builder
	for i := start; i < len(messages); i++ {
		m := messages[i]
		role := strings.ToUpper(m["role"])
		content := m["content"]
		b.WriteString(fmt.Sprintf("%s: %s\n", role, content))
	}
	return strings.TrimSpace(b.String())
}
