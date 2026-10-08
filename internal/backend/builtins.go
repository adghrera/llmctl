package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"llmctl/internal/backend/refstdio"
	"llmctl/internal/installer"
	"llmctl/internal/paths"
	"llmctl/internal/store"
)

// builtins returns the compiled-in backends.
func builtins() []Backend {
	return []Backend{
		&llamaCppBackend{},
		&ollamaBackend{},
		&vllmBackend{},
		&customBackend{},
		&refStdioBackend{},
	}
}

// ---- llama.cpp (http process) --------------------------------------------

type llamaCppBackend struct{}

const llamaCppRepo = "ggml-org/llama.cpp"

func (b *llamaCppBackend) ID() string          { return "llama.cpp" }
func (b *llamaCppBackend) Name() string        { return "llama.cpp" }
func (b *llamaCppBackend) Description() string { return "GGUF inference on CPU/CUDA/Vulkan (llama-server, OpenAI-compatible HTTP)" }
func (b *llamaCppBackend) Transport() string   { return TransportHTTP }
func (b *llamaCppBackend) Kind() string        { return KindProcess }

func (b *llamaCppBackend) Spec(m *store.ModelRecord, vars map[string]string) (Spec, error) {
	bin, err := installedBinary("llama.cpp", "llama-server"+OSExt())
	if err != nil {
		return Spec{}, err
	}
	v := defaults(vars, map[string]string{"ctx": "4096", "ngl": "0", "threads": "", "parallel": "4"})
	args := []string{
		"--port", "{port}",
		"--model", "{model}",
		"--host", "127.0.0.1",
		"--ctx-size", "{var:ctx}",
		"--n-gpu-layers", "{var:ngl}",
		"--parallel", "{var:parallel}",
	}
	if v["threads"] != "" {
		args = append(args, "--threads", v["threads"])
	}
	return Spec{
		Kind: KindProcess, Transport: TransportHTTP,
		Binary: bin, Args: args,
		HealthPath: "/health", HealthTimeoutS: 240,
		APIBasePath: "/v1", StopGraceS: 15,
	}, nil
}

func (b *llamaCppBackend) Health(inst *store.InstanceRecord) (HealthResult, error) {
	return genericHealth(TransportHTTP, inst, "/health", "/v1")
}

func (b *llamaCppBackend) Install(ctx context.Context, assetHint string, logf func(string)) (string, error) {
	return installer.Install(ctx, "llama.cpp", llamaCppRepo, assetHint, logf)
}

func (b *llamaCppBackend) Uninstall() error { return os.RemoveAll(paths.BackendDir("llama.cpp")) }

// ---- ollama (http attach) --------------------------------------------------

type ollamaBackend struct{}

func (b *ollamaBackend) ID() string          { return "ollama" }
func (b *ollamaBackend) Name() string        { return "Ollama" }
func (b *ollamaBackend) Description() string { return "Attach to a running Ollama daemon (OpenAI-compatible /v1)" }
func (b *ollamaBackend) Transport() string   { return TransportHTTP }
func (b *ollamaBackend) Kind() string        { return KindAttach }

func (b *ollamaBackend) Spec(m *store.ModelRecord, vars map[string]string) (Spec, error) {
	base := "http://127.0.0.1:11434"
	if v := vars["url"]; v != "" {
		base = v
	}
	return Spec{
		Kind: KindAttach, Transport: TransportHTTP,
		AttachURL: base, HealthPath: "/api/tags",
		APIBasePath: "/v1", StopGraceS: 0,
	}, nil
}

func (b *ollamaBackend) Health(inst *store.InstanceRecord) (HealthResult, error) {
	hr := genericHealth(TransportHTTP, inst, "/api/tags", "/v1")
	return hr, nil
}

func (b *ollamaBackend) Install(ctx context.Context, assetHint string, logf func(string)) (string, error) {
	return "external", nil
}

func (b *ollamaBackend) Uninstall() error { return nil }

// ---- vllm (http attach) ----------------------------------------------------

type vllmBackend struct{}

func (b *vllmBackend) ID() string          { return "vllm" }
func (b *vllmBackend) Name() string        { return "vLLM" }
func (b *vllmBackend) Description() string { return "Attach to a vLLM server (run: vllm serve <model>)" }
func (b *vllmBackend) Transport() string   { return TransportHTTP }
func (b *vllmBackend) Kind() string        { return KindAttach }

func (b *vllmBackend) Spec(m *store.ModelRecord, vars map[string]string) (Spec, error) {
	base := "http://127.0.0.1:8000"
	if v := vars["url"]; v != "" {
		base = v
	}
	return Spec{
		Kind: KindAttach, Transport: TransportHTTP,
		AttachURL: base, HealthPath: "/health",
		APIBasePath: "/v1", StopGraceS: 0,
	}, nil
}

func (b *vllmBackend) Health(inst *store.InstanceRecord) (HealthResult, error) {
	return genericHealth(TransportHTTP, inst, "/health", "/v1")
}

func (b *vllmBackend) Install(ctx context.Context, assetHint string, logf func(string)) (string, error) {
	return "external", nil
}

func (b *vllmBackend) Uninstall() error { return nil }

// ---- custom (http attach, user URL) ---------------------------------------

type customBackend struct{}

func (b *customBackend) ID() string          { return "custom" }
func (b *customBackend) Name() string        { return "Custom OpenAI-compatible" }
func (b *customBackend) Description() string { return "Any OpenAI-compatible HTTP server (LM Studio, TGI, remote, ...)" }
func (b *customBackend) Transport() string   { return TransportHTTP }
func (b *customBackend) Kind() string        { return KindAttach }

func (b *customBackend) Spec(m *store.ModelRecord, vars map[string]string) (Spec, error) {
	base := vars["url"]
	if base == "" {
		return Spec{}, fmt.Errorf("custom backend requires var url=http://host:port")
	}
	hp := "/health"
	if v := vars["healthPath"]; v != "" {
		hp = v
	}
	api := "/v1"
	if v := vars["apiPath"]; v != "" {
		api = v
	}
	return Spec{
		Kind: KindAttach, Transport: TransportHTTP,
		AttachURL: strings.TrimSuffix(base, "/"), HealthPath: hp,
		APIBasePath: api, StopGraceS: 0,
	}, nil
}

func (b *customBackend) Health(inst *store.InstanceRecord) (HealthResult, error) {
	return genericHealth(TransportHTTP, inst, "/health", "/v1")
}

func (b *customBackend) Install(ctx context.Context, assetHint string, logf func(string)) (string, error) {
	return "external", nil
}

func (b *customBackend) Uninstall() error { return nil }

// ---- ref-stdio (reference stdio backend, python) ---------------------------

type refStdioBackend struct{}

func (b *refStdioBackend) ID() string          { return "ref-stdio" }
func (b *refStdioBackend) Name() string        { return "Reference stdio backend" }
func (b *refStdioBackend) Description() string { return "llmctl-stdio/1 reference implementation (python, GGUF via llama.cpp CLI if available; echo fallback otherwise)" }
func (b *refStdioBackend) Transport() string   { return TransportStdio }
func (b *refStdioBackend) Kind() string        { return KindProcess }

func (b *refStdioBackend) Spec(m *store.ModelRecord, vars map[string]string) (Spec, error) {
	py := "python3"
	if runtime.GOOS == "windows" {
		py = "python"
	}
	if v := vars["python"]; v != "" {
		py = v
	}
	script := filepath.Join(paths.Home(), "backends", "ref-stdio", "ref_stdio_backend.py")
	if _, err := os.Stat(script); err != nil {
		return Spec{}, fmt.Errorf("reference stdio script missing at %s (run: llmctl backends install ref-stdio)", script)
	}
	args := []string{script, "--model", "{model}"}
	if v := vars["ctx"]; v != "" {
		args = append(args, "--ctx", v)
	}
	return Spec{
		Kind: KindProcess, Transport: TransportStdio,
		Binary: py, Args: args,
		StdioReadyTimeoutS: 30, StopGraceS: 5,
	}, nil
}

func (b *refStdioBackend) Health(inst *store.InstanceRecord) (HealthResult, error) {
	return HealthResult{Healthy: true, Models: []string{inst.ModelID}}, nil
}

func (b *refStdioBackend) Install(ctx context.Context, assetHint string, logf func(string)) (string, error) {
	// The script ships embedded; "install" just materializes it.
	return "embedded", materializeRefStdio(logf)
}

// materializeRefStdio writes the embedded reference stdio script to disk.
func materializeRefStdio(logf func(string)) error {
	dir := filepath.Join(paths.Home(), "backends", "ref-stdio")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, "ref_stdio_backend.py")
	if err := os.WriteFile(p, []byte(refstdio.Script), 0o755); err != nil {
		return err
	}
	if logf != nil {
		logf("wrote " + p)
	}
	return nil
}

func (b *refStdioBackend) Uninstall() error {
	return os.RemoveAll(filepath.Join(paths.Home(), "backends", "ref-stdio"))
}

// ---- shared helpers ---------------------------------------------------------

func defaults(vars, defs map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range defs {
		out[k] = v
	}
	for k, v := range vars {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

func installedBinary(backendID, name string) (string, error) {
	dir := paths.BackendDir(backendID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("backend %q not installed (run: llmctl backends install %s)", backendID, backendID)
	}
	var newest string
	var newestT time.Time
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cand := filepath.Join(dir, e.Name(), name)
		if _, err := os.Stat(cand); err == nil {
			t, err := e.Info().ModTime()
			if err == nil && (newest == "" || t.After(newestT)) {
				newest, newestT = cand, t
			}
		}
	}
	if newest == "" {
		return "", fmt.Errorf("binary %s not found under %s (run: llmctl backends install %s)", name, dir, backendID)
	}
	return newest, nil
}

// genericHealth does an HTTP GET on base+healthPath and, if apiPath is set,
// also fetches the model list.
func genericHealth(transport string, inst *store.InstanceRecord, healthPath, apiPath string) (HealthResult, error) {
	if transport != TransportHTTP {
		return HealthResult{Healthy: true}, nil
	}
	base := inst.BaseURL()
	if healthPath == "" {
		healthPath = "/health"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(base + healthPath)
	if err != nil {
		return HealthResult{Healthy: false, Error: err.Error()}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return HealthResult{Healthy: false, Error: fmt.Sprintf("health %s -> %d", healthPath, resp.StatusCode)}, nil
	}
	hr := HealthResult{Healthy: true}
	if apiPath != "" {
		if u, err := url.Parse(base + apiPath + "/models"); err == nil {
			if mresp, err := client.Get(u.String()); err == nil {
				defer mresp.Body.Close()
				var body struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				}
				if jsonErr := jsonUnmarshal(mresp.Body, &body); jsonErr == nil {
					for _, d := range body.Data {
						hr.Models = append(hr.Models, d.ID)
					}
				}
			}
		}
	}
	return hr, nil
}

// jsonUnmarshal decodes a JSON body.
func jsonUnmarshal(r interface{ Read([]byte) (int, error) }, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}
