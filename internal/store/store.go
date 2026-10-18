// Package store persists llmctl's state (installed backends, downloaded
// models, running instances) to a single JSON file with atomic writes and
// advisory file locking, so the CLI and the daemon never corrupt each other.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"llmctl/internal/paths"
)
// Status values shared across record types.
const (
	StatusInstalled = "installed"
	StatusRunning   = "running"
	StatusStopped   = "stopped"
	StatusError     = "error"
	StatusPartial   = "partial" // interrupted download, resumable
)

type BackendRecord struct {
	ID         string    `json:"id"`         // manifest backend id
	Version    string    `json:"version"`    // release tag or "external"
	BinaryPath string    `json:"binaryPath"` // absolute path, "" if on PATH
	InstalledAt time.Time `json:"installedAt"`
}

type ModelRecord struct {
	ID          string    `json:"id"`
	BackendID   string    `json:"backendId"`
	Path        string    `json:"path"` // .gguf file or model dir
	SizeBytes   int64     `json:"sizeBytes"`
	DownloadedAt time.Time `json:"downloadedAt"`
	// CtxSize is the default context window (tokens) used when starting
	// instances of this model; 0 = backend default. Overridable per start
	// via vars["ctx"].
	CtxSize int `json:"ctxSize,omitempty"`
}

// InstanceRecord is one launched (or attach-declared) serving endpoint.
type InstanceRecord struct {
	ID        string            `json:"id"`
	ModelID   string            `json:"modelId"`
	BackendID string            `json:"backendId"`
	Port      int               `json:"port"`
	// Transport: "http" (reverse-proxied) or "stdio" (IPC session).
	Transport   string          `json:"transport,omitempty"`
	// APIBasePath is the upstream OpenAI-compatible root, e.g. "/v1".
	APIBasePath string          `json:"apiBasePath"`
	Status    string            `json:"status"`
	Attached  bool              `json:"attached"` // process not owned by us
	PID       int               `json:"pid"`
	StartedAt time.Time         `json:"startedAt"`
	Vars      map[string]string `json:"vars,omitempty"`
	LogDir    string            `json:"logDir"`
	LastError string            `json:"lastError,omitempty"`
}

// BaseURL returns the HTTP base for an http-transport instance.
func (i *InstanceRecord) BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", i.Port)
}

// APIKeyRecord is a gateway API key, stored as a sha256 hash (plaintext is
// shown to the user exactly once, at creation).
type APIKeyRecord struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Hash      string    `json:"hash"` // sha256 hex of the plaintext key
	Prefix    string    `json:"prefix"` // first 12 chars, for display
	CreatedAt time.Time `json:"createdAt"`
	RateRPS   float64   `json:"rateRPS"` // sustained requests/sec per key (0 means use default)
	RateBurst int       `json:"rateBurst"` // max burst per key (0 means use default)
}

// Settings holds daemon-wide configuration.
type Settings struct {
	// RequireAuth controls whether the gateway enforces API key auth.
	// When false, no key is needed (anyone can use the gateway).
	// When true, a valid Bearer token is required for all /v1/* endpoints.
	RequireAuth bool `json:"requireAuth"`
}

// AutoStartSpec is a persistent "relaunch me at daemon start" declaration.
// Instance records are forgotten on stop, so the spec lives separately.
type AutoStartSpec struct {
	Name      string            `json:"name"`      // desired instance id ("" = derive from model)
	ModelID   string            `json:"modelId"`
	BackendID string            `json:"backendId,omitempty"`
	Port      int               `json:"port,omitempty"` // 0 = auto-assign
	Vars      map[string]string `json:"vars,omitempty"`
}

type State struct {
	Backends  map[string]*BackendRecord    `json:"backends"`
	Models    map[string]*ModelRecord      `json:"models"`
	Instances map[string]*InstanceRecord   `json:"instances"`
	APIKeys   map[string]*APIKeyRecord     `json:"apiKeys"`
	Settings  *Settings                    `json:"settings"`
	AutoStart map[string]*AutoStartSpec    `json:"autoStart,omitempty"` // keyed by model id
}

func newState() *State {
	return &State{
		Backends:  map[string]*BackendRecord{},
		Models:    map[string]*ModelRecord{},
		Instances: map[string]*InstanceRecord{},
		APIKeys:   map[string]*APIKeyRecord{},
		Settings:  &Settings{RequireAuth: true},
	}
}

// Store is a lock-guarded handle on state.json.
type Store struct {
	path string
	mu   fileLock
}

func Open() (*Store, error) {
	if err := paths.EnsureDirs(); err != nil {
		return nil, err
	}
	s := &Store{path: paths.StateFile(), mu: fileLock{path: paths.StateFile() + ".lock"}}
	return s, nil
}

// Load reads state under an exclusive lock.
func (s *Store) Load() (*State, error) {
	if err := s.mu.acquire(); err != nil {
		return nil, err
	}
	defer s.mu.release()
	return s.loadLocked()
}

func (s *Store) loadLocked() (*State, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return newState(), nil
	}
	if err != nil {
		return nil, err
	}
	st := newState()
	if err := json.Unmarshal(b, st); err != nil {
		return nil, fmt.Errorf("state.json corrupt: %w", err)
	}
	return st, nil
}

// Update runs fn under an exclusive lock and saves if it returns nil error.
func (s *Store) Update(fn func(*State) error) error {
	if err := s.mu.acquire(); err != nil {
		return err
	}
	defer s.mu.release()
	st, err := s.loadLocked()
	if err != nil {
		return err
	}
	if err := fn(st); err != nil {
		return err
	}
	return s.saveLocked(st)
}

// saveLocked writes atomically: temp file + rename.
func (s *Store) saveLocked(st *State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	// Rename over an existing file needs a remove-first on Windows.
	if _, err := os.Stat(s.path); err == nil {
		if err := os.Remove(s.path); err != nil {
			return err
		}
	}
	return os.Rename(tmp, s.path)
}

// SortedInstanceIDs returns instance IDs in stable order.
func (st *State) SortedInstanceIDs() []string {
	ids := make([]string, 0, len(st.Instances))
	for id := range st.Instances {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
