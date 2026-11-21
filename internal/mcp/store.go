// Package mcp is a minimal Model Context Protocol client: it manages
// user-configured MCP servers (stdio transport, newline-delimited JSON-RPC
// 2.0), lists their tools, and invokes them. Server configs live in
// $LLMCTL_HOME/mcp/servers.json.
package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"llmctl/internal/paths"
)

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Server is one configured MCP server.
type Server struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
}

// ToolDef is one tool advertised by a server (subset of MCP's schema).
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// Store manages the server registry file.
type Store struct {
	mu    sync.Mutex
	path  string
	m     map[string]*Server
	order []string
}

func dir() string { return filepath.Join(paths.Home(), "mcp") }

// Open loads (or creates) the server registry.
func Open() (*Store, error) {
	s := &Store{path: filepath.Join(dir(), "servers.json"), m: map[string]*Server{}}
	b, err := os.ReadFile(s.path)
	if err == nil {
		var list []*Server
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, fmt.Errorf("mcp: servers.json corrupt: %w", err)
		}
		for _, sv := range list {
			s.m[sv.ID] = sv
			s.order = append(s.order, sv.ID)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (s *Store) save() error {
	if err := os.MkdirAll(dir(), 0o755); err != nil {
		return err
	}
	list := make([]*Server, 0, len(s.order))
	for _, id := range s.order {
		list = append(list, s.m[id])
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns all servers in creation order.
func (s *Store) List() []*Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Server, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.m[id])
	}
	return out
}

// Get returns one server.
func (s *Store) Get(id string) (*Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("mcp: server %q not found", id)
	}
	return sv, nil
}

// Create adds a server. Name and Command are required.
func (s *Store) Create(in Server) (*Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.Name = strings.TrimSpace(in.Name)
	in.Command = strings.TrimSpace(in.Command)
	if in.Name == "" || in.Command == "" {
		return nil, fmt.Errorf("mcp: name and command are required")
	}
	if in.ID == "" {
		in.ID = genID()
	}
	if !safeID.MatchString(in.ID) {
		return nil, fmt.Errorf("mcp: invalid id %q", in.ID)
	}
	if _, exists := s.m[in.ID]; exists {
		return nil, fmt.Errorf("mcp: server %q already exists", in.ID)
	}
	cp := in
	s.m[in.ID] = &cp
	s.order = append(s.order, in.ID)
	if err := s.save(); err != nil {
		delete(s.m, in.ID)
		return nil, err
	}
	return &cp, nil
}

// Delete removes a server.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[id]; !ok {
		return fmt.Errorf("mcp: server %q not found", id)
	}
	delete(s.m, id)
	for i, x := range s.order {
		if x == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return s.save()
}

// SetEnabled toggles a server.
func (s *Store) SetEnabled(id string, enabled bool) (*Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("mcp: server %q not found", id)
	}
	sv.Enabled = enabled
	if err := s.save(); err != nil {
		return nil, err
	}
	return sv, nil
}

func genID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("mcp-%d", time.Now().UnixNano())
	}
	return "mcp-" + hex.EncodeToString(b)
}
