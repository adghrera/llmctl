// Package manifest defines the registry of supported inference backends
// and model sources. Backends are external binaries (llama.cpp, vLLM,
// Ollama, ...) that expose an OpenAI-compatible HTTP API on a local port.
package manifest

import (
	"fmt"
	"runtime"
	"strings"
)

// BinaryRef identifies where a backend binary comes from.
type BinaryRef struct {
	// Kind is "github-release" (download an archive), "url" (direct
	// download), or "path" (already on PATH, no install needed).
	Kind string `json:"kind"`
	// URL for kind=url, or release archive URL template for github-release.
	URL string `json:"url,omitempty"`
	// AssetMatch is a substring used to pick the right asset from a
	// GitHub release's asset list (e.g. "win-cuda-cu12.4" for llama.cpp).
	AssetMatch map[string]string `json:"assetMatch,omitempty"` // key: GOOS ("windows","linux","darwin")
	// Extract: "zip", "tar.gz", or "" for none.
	Extract string `json:"extract,omitempty"`
	// BinaryName is the executable to look for after extraction.
	BinaryName string `json:"binaryName"`
	// Subdir is an optional path inside the archive where the binary lives.
	Subdir string `json:"subdir,omitempty"`
}

// VarSpec describes one templated argument.
type VarSpec struct {
	Name     string `json:"name"`
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// RuntimeSpec tells llmctl how to launch and talk to one backend.
type RuntimeSpec struct {
	// PortFlag is the flag template used to bind the serving port,
	// e.g. "--port {port}". The proxy always connects to 127.0.0.1:{port}.
	PortFlag string `json:"portFlag"`
	// ModelFlag binds a model, e.g. "--model {model}".
	ModelFlag string `json:"modelFlag"`
	// ArgsTemplate is extra CLI args, {var} placeholders allowed.
	ArgsTemplate string `json:"argsTemplate,omitempty"`
	// Vars are the templated variables users can override per-install.
	Vars []VarSpec `json:"vars,omitempty"`
	// HealthPath polled until 200 before the instance is marked running.
	HealthPath string `json:"healthPath"`
	// HealthTimeoutS bounds the startup health wait.
	HealthTimeoutS int `json:"healthTimeoutS,omitempty"`
	// StopGraceS: SIGTERM grace period before force-kill.
	StopGraceS int `json:"stopGraceS,omitempty"`
	// EnvExtra injected into the child process, KEY=VAL list.
	EnvExtra []string `json:"envExtra,omitempty"`
	// ChatCompletionsPath overrides the upstream API path if non-default.
	ChatCompletionsPath string `json:"chatCompletionsPath,omitempty"`
}

// ModelSource describes where a model artifact lives.
type ModelSource struct {
	// Kind: "huggingface" (resolved via HF API) or "url" (direct).
	Kind string `json:"kind"`
	// Repo (org/name) for huggingface, or filename.
	Repo string `json:"repo,omitempty"`
	Filename string `json:"filename,omitempty"`
	// Revision of the HF repo (default "main").
	Revision string `json:"revision,omitempty"`
	// Quant is a human label, e.g. "Q4_K_M".
	Quant string `json:"quant,omitempty"`
	// License note shown in listings.
	License string `json:"license,omitempty"`
}

// BackendDef is one entry of the backend registry.
type BackendDef struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	// Formats this backend serves (gguf, safetensors, ...). Empty = any.
	Formats []string `json:"formats,omitempty"`
	// RequiresGpu is advisory: install warns if no GPU detected.
	RequiresGpu bool `json:"requiresGpu,omitempty"`
	Binary       BinaryRef  `json:"binary"`
	Runtime      RuntimeSpec `json:"runtime"`
}

// ModelDef is one entry of the curated model registry.
type ModelDef struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Backend     string      `json:"backend"` // backend ID
	Format      string      `json:"format"`
	SizeGB      float64     `json:"sizeGB"`
	MinVRAMGB   float64     `json:"minVRAMGB,omitempty"`
	Source      ModelSource `json:"source"`
	ContextLength int       `json:"contextLength,omitempty"`
}

// Registry is the whole catalog, embedded into the binary.
type Registry struct {
	Backends []BackendDef `json:"backends"`
	Models   []ModelDef   `json:"models"`
}

// Builtin returns the embedded registry (manifest.json is go:embedded).
func Builtin() (*Registry, error) {
	data, err := manifestFS.ReadFile("manifest.json")
	if err != nil {
		return nil, err
	}
	reg := &Registry{}
	if err := jsonUnmarshal(data, reg); err != nil {
		return nil, fmt.Errorf("parse embedded manifest: %w", err)
	}
	return reg, nil
}

// Backend looks up a backend by ID.
func (r *Registry) Backend(id string) (*BackendDef, bool) {
	for i := range r.Backends {
		if strings.EqualFold(r.Backends[i].ID, id) {
			return &r.Backends[i], true
		}
	}
	return nil, false
}

// Model looks up a model by ID.
func (r *Registry) Model(id string) (*ModelDef, bool) {
	for i := range r.Models {
		if strings.EqualFold(r.Models[i].ID, id) {
			return &r.Models[i], true
		}
	}
	return nil, false
}

// PlatformKey is the current GOOS, used to pick release assets.
func PlatformKey() string { return runtime.GOOS }
