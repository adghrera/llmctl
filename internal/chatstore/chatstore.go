// Package chatstore persists chat conversations as one JSON file per
// conversation under $LLMCTL_HOME/chats/. Files (not state.json) keep the
// hot state file small and make manual backup/inspection trivial.
package chatstore

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

// safeID: 16 hex chars. IDs are generated, never taken from user text.
var safeID = regexp.MustCompile(`^[0-9a-f]{16}$`)

type Message struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
	// Tools records agent tool activity (searches) shown inline in the UI.
	Tools []ToolEvent `json:"tools,omitempty"`
}

type ToolEvent struct {
	Name    string `json:"name,omitempty"`
	Query   string `json:"query,omitempty"`
	Results any    `json:"results,omitempty"`
}

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Model     string    `json:"model,omitempty"`
	AgentMode bool      `json:"agentMode,omitempty"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Summary is the list-view shape (no message bodies).
type Summary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Model     string    `json:"model,omitempty"`
	AgentMode bool      `json:"agentMode,omitempty"`
	Messages  int       `json:"messages"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Store struct{ dir string }

func Open() (*Store, error) {
	dir := filepath.Join(paths.Home(), "chats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("chat dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }

// TitleFrom derives a conversation title from the first user message.
func TitleFrom(messages []Message) string {
	for _, m := range messages {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			t := strings.TrimSpace(strings.Join(strings.Fields(m.Content), " "))
			if len(t) > 60 {
				// avoid cutting a UTF-8 rune in half
				cut := 60
				for cut > 0 && t[cut]&0xC0 == 0x80 {
					cut--
				}
				t = strings.TrimSpace(t[:cut]) + "…"
			}
			return t
		}
	}
	return "New chat"
}

// Save writes the conversation (creating it if ID is empty) and returns
// the stored copy with ID/timestamps populated.
func (s *Store) Save(c *Conversation) (*Conversation, error) {
	if c.ID == "" {
		c.ID = newID()
	} else if !safeID.MatchString(c.ID) {
		return nil, fmt.Errorf("invalid chat id")
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if strings.TrimSpace(c.Title) == "" {
		c.Title = TitleFrom(c.Messages)
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	tmp := s.path(c.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, s.path(c.ID)); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	return c, nil
}

func (s *Store) Get(id string) (*Conversation, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid chat id")
	}
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not found")
		}
		return nil, err
	}
	var c Conversation
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("chat %s corrupt: %w", id, err)
	}
	return &c, nil
}

// List returns all conversations, newest-updated first.
func (s *Store) List() ([]Summary, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if !safeID.MatchString(id) {
			continue
		}
		c, err := s.Get(id)
		if err != nil {
			continue // skip corrupt files in listings
		}
		out = append(out, Summary{ID: c.ID, Title: c.Title, Model: c.Model, AgentMode: c.AgentMode, Messages: len(c.Messages), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *Store) Delete(id string) error {
	if !safeID.MatchString(id) {
		return fmt.Errorf("invalid chat id")
	}
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return fmt.Errorf("not found")
	}
	return err
}
