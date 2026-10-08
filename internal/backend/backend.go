// Package backend defines the pluggable backend system.
//
// A Backend knows how to (a) install its runtime, (b) build the launch spec
// for a model, (c) talk to a running instance, and (d) health-check it.
//
// Backends are pluggable in two ways:
//  1. Built-in backends compiled into llmctl (llama.cpp, ollama, vllm, custom,
//     and the reference stdio backend).
//  2. Plugin backends declared as JSON files in $LLMCTL_HOME/plugins/*.json —
//     no recompilation needed. Plugins can describe HTTP or stdio backends,
//     with installers for direct URLs, GitHub releases, or plain file lists.
//
// Transports:
//   - "http":  the backend exposes an OpenAI-compatible HTTP API (llama.cpp,
//     vLLM, ollama, LM Studio, ...). llmctl reverse-proxies it.
//   - "stdio": the backend speaks the llmctl-stdio/1 JSON-lines protocol on
//     stdin/stdout. llmctl launches it and multiplexes many concurrent
//     requests over the pipe — no port, no HTTP overhead.
package backend

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"llmctl/internal/store"
)

// Transport kinds.
const (
	TransportHTTP  = "http"
	TransportStdio = "stdio"
)

// Instance kinds.
const (
	KindProcess = "process" // llmctl launches and owns the process
	KindAttach  = "attach"  // user runs it; llmctl connects to a fixed address
)

// Spec describes how to launch (or connect to) a backend for one model.
type Spec struct {
	Kind      string            `json:"kind"` // process | attach
	Transport string            `json:"transport"`
	Binary    string            `json:"binary,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	// Attach (http only): fixed endpoint.
	AttachURL string `json:"attachUrl,omitempty"`
	// HTTP settings.
	HealthPath     string `json:"healthPath,omitempty"`
	HealthTimeoutS int    `json:"healthTimeoutS,omitempty"`
	APIBasePath    string `json:"apiBasePath,omitempty"`
	// Stdio settings.
	StdioReadyTimeoutS int `json:"stdioReadyTimeoutS,omitempty"`
	// StopGraceS: seconds to wait after stop signal before force kill.
	StopGraceS int `json:"stopGraceS,omitempty"`
}

// HealthResult is the outcome of a backend health check.
type HealthResult struct {
	Healthy bool
	Models  []string // model names the backend reports
	Error   string
}

// Backend is the pluggable interface.
type Backend interface {
	ID() string
	Name() string
	Description() string
	// Transport reports the primary transport for this backend.
	Transport() string
	// Kind reports process or attach.
	Kind() string
	// Spec builds the launch spec for model m (m.Path is absolute).
	// vars are user overrides (ctx, ngl, threads, ...).
	Spec(m *store.ModelRecord, vars map[string]string) (Spec, error)
	// Health checks a running instance.
	Health(inst *store.InstanceRecord) (HealthResult, error)
	// Install fetches the backend runtime. Returns the version string.
	// Attach backends return ("external", nil).
	Install(ctx context.Context, assetHint string, logf func(string)) (string, error)
	// Uninstall removes installed runtime files (best effort).
	Uninstall() error
}

// ---- shared helpers -------------------------------------------------------

// renderArgs expands {model}, {port}, {dir}, and {var:key} placeholders.
func renderArgs(args []string, model, dir string, vars map[string]string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		a = strings.ReplaceAll(a, "{model}", model)
		a = strings.ReplaceAll(a, "{dir}", dir)
		for k, v := range vars {
			a = strings.ReplaceAll(a, "{var:"+k+"}", v)
		}
		out[i] = a
	}
	return out
}

// OSExt returns the executable suffix for the current platform.
func OSExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// OSArch returns lowercase os-arch like "linux-amd64", "windows-amd64".
func OSArch() string {
	return strings.ToLower(runtime.GOOS + "-" + runtime.GOARCH)
}

// OSExtToken maps GOOS to common asset tokens.
func OSExtToken() string {
	switch runtime.GOOS {
	case "windows":
		return "win"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
}

// ModelExt returns the expected model file extension for a backend.
func ModelExt(transport string) string {
	if transport == TransportStdio {
		return ".gguf"
	}
	return ".gguf"
}

// validateSpec catches common misconfigurations early.
func validateSpec(s Spec) error {
	switch s.Kind {
	case KindProcess:
		if s.Binary == "" {
			return fmt.Errorf("process backend needs a binary")
		}
	case KindAttach:
		if s.Transport == TransportHTTP && s.AttachURL == "" {
			return fmt.Errorf("attach backend needs an attachUrl")
		}
	default:
		return fmt.Errorf("unknown kind %q", s.Kind)
	}
	switch s.Transport {
	case TransportHTTP, TransportStdio:
	default:
		return fmt.Errorf("unknown transport %q", s.Transport)
	}
	if s.HealthTimeoutS <= 0 {
		s.HealthTimeoutS = 120
	}
	if s.StopGraceS <= 0 {
		s.StopGraceS = 10
	}
	return nil
}
