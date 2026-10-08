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
}

// InstanceRecord is one launched (or attach-declared) serving endpoint.
type InstanceRecord struct {
	ID        string            `json:"id"`
	ModelID   string            `json:"modelId"`
	BackendID string            `json:"backendId"`
	Port      int               `json:"port"`
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

type State struct {
	Backends map[string]*BackendRecord  `json:"backends"`
	Models   map[string]*ModelRecord    `json:"models"`
	Instances map[string]*InstanceRecord `json:"instances"`
}

func newState() *State {
	return &State{Backends: map[string]*BackendRecord{}, Models: map[string]*ModelRecord{}, Instances: map[string]*InstanceRecord{}}
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
