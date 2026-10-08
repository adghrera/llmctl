package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"llmctl/internal/paths"
	"llmctl/internal/store"
)

// Registry holds all known backends: built-ins plus loaded plugins.
type Registry struct {
	mu       sync.RWMutex
	backends map[string]Backend
	pluginM  map[string]*PluginMeta // plugin id -> metadata (for UI)
}

// PluginMeta is the on-disk plugin descriptor.
type PluginMeta struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"` // http | stdio
	Kind        string            `json:"kind"`      // process | attach
	Binary      string            `json:"binary,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	AttachURL   string            `json:"attachUrl,omitempty"`
	HealthPath  string            `json:"healthPath,omitempty"`
	HealthTimeoutS int            `json:"healthTimeoutS,omitempty"`
	APIBasePath string            `json:"apiBasePath,omitempty"`
	StdioReadyTimeoutS int        `json:"stdioReadyTimeoutS,omitempty"`
	StopGraceS  int               `json:"stopGraceS,omitempty"`
	// Installer: how to obtain the binary.
	Install *PluginInstall `json:"install,omitempty"`
	// SourceFile: path of the plugin JSON (for UI + removal).
	SourceFile string `json:"-"`
}

// PluginInstall describes how a plugin backend downloads its runtime.
type PluginInstall struct {
	Kind string `json:"kind"` // url | github-release | files
	// url: single binary download.
	URL string `json:"url,omitempty"` // may contain {os}, {arch}, {ext}
	// github-release.
	Repo       string            `json:"repo,omitempty"`
	AssetMatch map[string]string `json:"assetMatch,omitempty"` // GOOS -> substring
	Extract    string            `json:"extract,omitempty"`    // zip | tar.gz | ""
	BinaryName string            `json:"binaryName,omitempty"`
	// files: list of {name, url} to fetch.
	Files []PluginFile `json:"files,omitempty"`
}

type PluginFile struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// NewRegistry loads built-ins and plugins from the plugins dir.
func NewRegistry() (*Registry, error) {
	r := &Registry{backends: map[string]Backend{}, pluginM: map[string]*PluginMeta{}}
	for _, b := range builtins() {
		r.backends[b.ID()] = b
	}
	if err := r.LoadPlugins(); err != nil {
		return nil, err
	}
	return r, nil
}

// LoadPlugins (re)scans $LLMCTL_HOME/plugins/*.json.
func (r *Registry) LoadPlugins() error {
	dir := paths.PluginsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Drop previously loaded plugins first.
	r.mu.Lock()
	for id := range r.pluginM {
		delete(r.backends, id)
		delete(r.pluginM, id)
	}
	r.mu.Unlock()

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m PluginMeta
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("plugin %s: %w", e.Name(), err)
		}
		if m.ID == "" {
			return fmt.Errorf("plugin %s: missing id", e.Name())
		}
		m.SourceFile = p
		impl, err := newPluginBackend(&m)
		if err != nil {
			return fmt.Errorf("plugin %s: %w", e.Name(), err)
		}
		r.mu.Lock()
		r.backends[m.ID] = impl
		r.pluginM[m.ID] = &m
		r.mu.Unlock()
	}
	return nil
}

// AddPlugin writes a plugin JSON and loads it.
func (r *Registry) AddPlugin(m *PluginMeta) error {
	if m.ID == "" {
		return fmt.Errorf("plugin id required")
	}
	if m.Transport == "" {
		m.Transport = TransportHTTP
	}
	if m.Kind == "" {
		m.Kind = KindProcess
	}
	if err := paths.EnsurePluginsDir(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(paths.PluginsDir(), m.ID+".json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return err
	}
	return r.LoadPlugins()
}

// RemovePlugin deletes the plugin file and reloads.
func (r *Registry) RemovePlugin(id string) error {
	r.mu.RLock()
	m, ok := r.pluginM[id]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin %q not found", id)
	}
	if err := os.Remove(m.SourceFile); err != nil {
		return err
	}
	return r.LoadPlugins()
}

func (r *Registry) Get(id string) (Backend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.backends[id]
	return b, ok
}

func (r *Registry) List() []Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Backend, 0, len(r.backends))
	for _, b := range r.backends {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

func (r *Registry) PluginMeta(id string) *PluginMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pluginM[id]
}

// IsPlugin reports whether id is a plugin (not built-in).
func (r *Registry) IsPlugin(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.pluginM[id]
	return ok
}

// newPluginBackend wraps a PluginMeta as a Backend.
func newPluginBackend(m *PluginMeta) (Backend, error) {
	if m.Transport != TransportHTTP && m.Transport != TransportStdio {
		return nil, fmt.Errorf("unknown transport %q", m.Transport)
	}
	if m.Kind != KindProcess && m.Kind != KindAttach {
		return nil, fmt.Errorf("unknown kind %q", m.Kind)
	}
	if m.Kind == KindProcess && m.Binary == "" {
		return nil, fmt.Errorf("process plugin needs a binary name")
	}
	if m.Kind == KindAttach && m.Transport == TransportHTTP && m.AttachURL == "" {
		return nil, fmt.Errorf("attach plugin needs attachUrl")
	}
	return &pluginBackend{m: m}, nil
}

type pluginBackend struct{ m *PluginMeta }

func (p *pluginBackend) ID() string          { return p.m.ID }
func (p *pluginBackend) Name() string        { return p.m.Name }
func (p *pluginBackend) Description() string { return p.m.Description }
func (p *pluginBackend) Transport() string   { return p.m.Transport }
func (p *pluginBackend) Kind() string        { return p.m.Kind }

func (p *pluginBackend) Spec(m *store.ModelRecord, vars map[string]string) (Spec, error) {
	s := Spec{
		Kind:             p.m.Kind,
		Transport:        p.m.Transport,
		Binary:           p.m.Binary,
		Args:             renderArgs(p.m.Args, m.Path, filepath.Dir(m.Path), vars),
		Env:              p.m.Env,
		AttachURL:        p.m.AttachURL,
		HealthPath:       p.m.HealthPath,
		HealthTimeoutS:   p.m.HealthTimeoutS,
		APIBasePath:      p.m.APIBasePath,
		StdioReadyTimeoutS: p.m.StdioReadyTimeoutS,
		StopGraceS:       p.m.StopGraceS,
	}
	if err := validateSpec(s); err != nil {
		return Spec{}, err
	}
	return s, nil
}

func (p *pluginBackend) Health(inst *store.InstanceRecord) (HealthResult, error) {
	return genericHealth(p.m.Transport, inst, p.m.HealthPath, p.m.APIBasePath)
}

func (p *pluginBackend) Install(ctx context.Context, assetHint string, logf func(string)) (string, error) {
	if p.m.Kind == KindAttach {
		return "external", nil
	}
	if p.m.Install == nil {
		return "external", nil
	}
	return runPluginInstall(ctx, p.m, assetHint, logf)
}

func (p *pluginBackend) Uninstall() error {
	return os.RemoveAll(paths.BackendDir(p.m.ID))
}
