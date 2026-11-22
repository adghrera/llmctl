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
	// PriceInPerM / PriceOutPerM are the estimated USD cost per 1M tokens,
	// used for per-key cost tracking (F102). 0 = free/local.
	PriceInPerM  float64 `json:"priceInPerM,omitempty"`
	PriceOutPerM float64 `json:"priceOutPerM,omitempty"`
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
	// HealthURL is the probed endpoint for attached instances; empty for
	// owned ones (their process is reaped directly) (F096).
	HealthURL string            `json:"healthUrl,omitempty"`
	PID       int               `json:"pid"`
	StartedAt time.Time         `json:"startedAt"`
	Vars      map[string]string `json:"vars,omitempty"`
	LogDir    string            `json:"logDir"`
	LastError string            `json:"lastError,omitempty"`
	// AutoRestart relaunches the instance when it crashes (owned processes
	// only). Crash-loop guard: max 3 restarts per 5 minutes (F087).
	AutoRestart bool `json:"autoRestart,omitempty"`
	// MaxParallel caps simultaneous gateway requests served by this
	// instance; 0 = unlimited (F114).
	MaxParallel int `json:"maxParallel,omitempty"`
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
	// TokenQuota caps total tokens (prompt+completion) this key may spend
	// since daemon start; 0 = unlimited (F074).
	TokenQuota int64 `json:"tokenQuota,omitempty"`
	// AllowedModels restricts which gateway models this key may call;
	// empty = all models (F089).
	AllowedModels []string `json:"allowedModels,omitempty"`
	// MaxConcurrent caps simultaneous in-flight requests for this key;
	// 0 = unlimited (F095).
	MaxConcurrent int `json:"maxConcurrent,omitempty"`
	// MaxRequests caps the total number of requests this key may make;
	// 0 = unlimited (F105).
	MaxRequests int64 `json:"maxRequests,omitempty"`
}

// Settings holds daemon-wide configuration.
type Settings struct {
	// RequireAuth controls whether the gateway enforces API key auth.
	// When false, no key is needed (anyone can use the gateway).
	// When true, a valid Bearer token is required for all /v1/* endpoints.
	RequireAuth bool `json:"requireAuth"`
	// IdleUnloadMin auto-stops running instances that have served no gateway
	// request for this many minutes (0 = disabled). VRAM relief for hosts
	// that host many models but use few (F060).
	IdleUnloadMin int `json:"idleUnloadMin"`
	// NotifyWebhook receives JSON POSTs for instance events (crashed,
	// idle-unloaded). Empty = disabled (F073).
	NotifyWebhook string `json:"notifyWebhook,omitempty"`
	// NotifyWebhookSecret, when set, makes every webhook POST carry an
	// HMAC-SHA256 signature header X-LLMCtrl-Signature (F111).
	NotifyWebhookSecret string `json:"notifyWebhookSecret,omitempty"`
	// AllowCORS makes the gateway answer OPTIONS preflights and send
	// Access-Control-Allow-* headers so browser apps can call it directly.
	AllowCORS bool `json:"allowCORS"`
	// StartupTimeoutSec bounds how long an instance start waits for the
	// backend to become healthy (0 = default 240 s). Raise for very large
	// models on slow storage (F085).
	StartupTimeoutSec int `json:"startupTimeoutSec"`
	// StartOnDemand lets a gateway request start a stopped instance for
	// the requested model (cold start); the request waits for health (F100).
	StartOnDemand bool `json:"startOnDemand"`
	// MaxBodyMB caps gateway request body size in MiB; 0 = 64 default (F121).
	MaxBodyMB int `json:"maxBodyMB,omitempty"`
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

// PresetSpec is a saved instance configuration (name → how to start it).
// Starting a preset goes through the normal start-instance path, so
// per-start vars still override the preset's.
type PresetSpec struct {
	Name      string            `json:"name"` // unique preset name
	Instance  string            `json:"instance,omitempty"`
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
	Presets   map[string]*PresetSpec       `json:"presets,omitempty"`   // keyed by preset name
	Aliases   map[string]string            `json:"aliases,omitempty"`   // gateway alias -> model id (F065)
	KeyUsage  map[string]*KeyUsageRecord `json:"keyUsage,omitempty"`  // per key prefix, flushed periodically (F080)
	// DailyUsage is UTC date ("2006-01-02") -> model id -> counters,
	// flushed with the key-usage loop so usage history survives restarts (F097).
	DailyUsage map[string]map[string]*DailyModelUsage `json:"dailyUsage,omitempty"`
	// InstanceNotes is a free-text annotation per instance id. It lives here
	// rather than on the instance record because records are forgotten when
	// an instance stops (F119).
	InstanceNotes map[string]string `json:"instanceNotes,omitempty"`
}

// KeyUsageRecord is the persisted token usage for one API key prefix, so
// per-key quotas (F074) survive daemon restarts (F080).
type KeyUsageRecord struct {
	Input    int64   `json:"input"`
	Output   int64   `json:"output"`
	Total    int64   `json:"total"`
	Requests int64   `json:"requests"`
	Cost     float64 `json:"cost,omitempty"` // estimated USD (F102)
}

// DailyModelUsage is one day's gateway usage for one model (F097).
type DailyModelUsage struct {
	Requests int64   `json:"requests"`
	Errors   int64   `json:"errors"`
	Prompt   int64   `json:"prompt"`
	Comp     int64   `json:"comp"`
	Cost     float64 `json:"cost,omitempty"` // estimated USD (F103)
}

func newState() *State {
	return &State{
		Backends:  map[string]*BackendRecord{},
		Models:    map[string]*ModelRecord{},
		Instances: map[string]*InstanceRecord{},
		APIKeys:   map[string]*APIKeyRecord{},
		Settings:  &Settings{RequireAuth: true},
		Aliases:   map[string]string{},
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
