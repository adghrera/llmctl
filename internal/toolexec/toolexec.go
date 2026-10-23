// Package toolexec runs installed tools.
//
// Two execution modes:
//
//   - host ("direct"): the tool runs as a local process. python entries run
//     through a small runner that imports the entry module and calls
//     run(args); command entries run as-is. Args JSON goes in on stdin,
//     a JSON result envelope comes out on stdout.
//
//   - sandbox: the tool is shipped into the llm-sandbox framework
//     (vndee/llm-sandbox, Python library) which runs it inside a container
//     (podman/docker backend) with no network, memory and PID caps, and
//     no-new-privileges. Go talks to it through an embedded bridge script:
//     everything the tool needs (source files + args) is embedded in the
//     generated sandbox program, so no file-copy APIs are required.
package toolexec

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"llmctl/internal/toolstore"
)

const (
	DefaultTimeout = 60 * time.Second
	maxOutput      = 1 << 20 // 1 MiB
)

// Result is what a tool run produces.
type Result struct {
	OK       bool   `json:"ok"`
	Result   string `json:"result,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exitCode"`
	Error    string `json:"error,omitempty"`
	Sandbox  bool   `json:"sandbox"`
	Backend  string `json:"backend,omitempty"`
	Duration string `json:"duration"`
}

// Run executes a tool with the given args JSON.
func Run(ctx context.Context, rec *toolstore.Record, argsJSON string, timeout time.Duration) *Result {
	start := time.Now()
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if rec.SandboxEnabled() {
		res := runSandboxed(ctx, rec, argsJSON, timeout)
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res
	}
	res := runHost(ctx, rec, argsJSON, timeout)
	res.Duration = time.Since(start).Round(time.Millisecond).String()
	return res
}

// --- host execution ---------------------------------------------------------

// runnerPy imports the entry module and calls run(args).
const runnerPy = `
import sys, json, base64, importlib.util, io
entry = sys.argv[1]
args = json.loads(base64.b64decode(sys.argv[2]).decode("utf-8")) if len(sys.argv) > 2 and sys.argv[2] else {}
spec = importlib.util.spec_from_file_location("llmctl_tool", entry)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
out = mod.run(args)
if not isinstance(out, str):
    out = json.dumps(out)
sys.stdout.write("\n__LLMCTL_RESULT__" + base64.b64encode(out.encode("utf-8")).decode("ascii"))
`

func pythonExe() (string, error) {
	for _, name := range []string{"python", "python3", "py"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no python interpreter found on PATH")
}

func runHost(ctx context.Context, rec *toolstore.Record, argsJSON string, timeout time.Duration) *Result {
	entry := filepath.Join(rec.Dir, rec.Manifest.Entry)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	switch rec.Manifest.Runtime {
	case "command":
		cmd = exec.CommandContext(ctx, entry)
		cmd.Dir = rec.Dir
	default: // python
		py, err := pythonExe()
		if err != nil {
			return &Result{Error: err.Error(), Sandbox: false}
		}
		b64 := base64.StdEncoding.EncodeToString([]byte(orEmptyArgs(argsJSON)))
		cmd = exec.CommandContext(ctx, py, "-c", runnerPy, entry, b64)
		cmd.Dir = rec.Dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{buf: &stdout}
	cmd.Stderr = &limitWriter{buf: &stderr}
	err := cmd.Run()
	res := &Result{Sandbox: false, Stderr: truncate(stderr.String(), 4000)}
	if ctx.Err() == context.DeadlineExceeded {
		res.Error = fmt.Sprintf("tool timed out after %s", timeout)
		return res
	}
	if err != nil {
		res.ExitCode = 1
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			res.Error = err.Error()
			return res
		}
		if res.ExitCode != 0 {
			if res.Stderr != "" {
				res.Error = "tool failed: " + firstLine(res.Stderr)
			} else {
				res.Error = fmt.Sprintf("tool exited with code %d", res.ExitCode)
			}
			return res
		}
	}
	out := stdout.String()
	if idx := strings.LastIndex(out, "__LLMCTL_RESULT__"); idx >= 0 {
		payload := strings.TrimSpace(out[idx+len("__LLMCTL_RESULT__"):])
		if dec, derr := base64.StdEncoding.DecodeString(payload); derr == nil {
			res.OK = true
			res.Result = truncate(string(dec), maxOutput)
			return res
		}
	}
	// command runtime (or legacy): stdout is the result
	res.OK = true
	res.Result = truncate(strings.TrimSpace(out), maxOutput)
	return res
}

// --- sandbox execution (llm-sandbox framework) ------------------------------

// bridgePy runs inside our host python and drives llm_sandbox.SandboxSession.
// Input: one JSON object on stdin (bridgeInput). Output: JSON Result on stdout.
const bridgePy = `
import sys, json, base64, os
# Windows hosts have no os.getuid; llm-sandbox maps host uid/gid on session
# setup. Shim POSIX-only identity calls before import (container runs as its
# own non-root user anyway).
if not hasattr(os, "getuid"):
    os.getuid = lambda: 1000
if not hasattr(os, "getgid"):
    os.getgid = lambda: 1000
if not hasattr(os, "geteuid"):
    os.geteuid = lambda: 1000
if not hasattr(os, "getgroups"):
    os.getgroups = lambda: [1000]
def fail(msg):
    print(json.dumps({"ok": False, "error": str(msg)}))
    sys.exit(0)
def pick_docker_host():
    # On Windows there is no AF_UNIX; probe named pipes that speak the
    # Docker API (Docker Desktop, or podman machine's pipe) and point
    # DOCKER_HOST at the first live one.
    if os.environ.get("DOCKER_HOST"):
        return
    if os.name != "nt":
        return
    for url in [
        "npipe:////./pipe/dockerDesktopLinuxEngine",
        "npipe:////./pipe/docker_engine",
        "npipe:////./pipe/podman-machine-default",
    ]:
        try:
            import docker as _d
            _d.DockerClient(base_url=url, timeout=5).ping()
            os.environ["DOCKER_HOST"] = url
            return
        except Exception:
            continue
_ = pick_docker_host()
try:
    req = json.loads(sys.stdin.buffer.read().decode("utf-8"))
    from llm_sandbox import SandboxSession
    files = req["files"]           # name -> base64
    runtime_configs = {
        "network_mode": "none",
        "mem_limit": "512m",
        "pids_limit": 128,
        "security_opt": ["no-new-privileges"],
    }
    # Tools that declare "network": true in their manifest get egress;
    # everything else stays fully isolated. Caps and no-new-privileges hold
    # either way.
    if req.get("network"):
        runtime_configs["network_mode"] = "bridge"
    runtime_configs.update(req.get("runtime_configs") or {})
    # podman's Docker-API accepts "no-new-privileges" but not ":true"; if the
    # backend rejects the option, retry without it rather than failing the run.
    setup = []
    setup.append("import os, base64, json, sys")
    for name, b64 in files.items():
        setup.append("_d = base64.b64decode(%r)" % b64)
        setup.append("open(%r, 'wb').write(_d)" % name)
    setup.append("sys.path.insert(0, '.')")
    setup.append("import importlib.util")
    setup.append("_spec = importlib.util.spec_from_file_location('llmctl_tool', %r)" % req["entry"])
    setup.append("_mod = importlib.util.module_from_spec(_spec); _spec.loader.exec_module(_mod)")
    setup.append("_args = json.loads(base64.b64decode(%r).decode('utf-8'))" % base64.b64encode(req["args"].encode("utf-8")).decode())
    setup.append("_out = _mod.run(_args)")
    setup.append("print('__LLMCTL_RESULT__' + base64.b64encode((_out if isinstance(_out, str) else json.dumps(_out)).encode('utf-8')).decode('ascii'))")
    program = "\n".join(setup)
    def _open(rc):
        return SandboxSession(lang="python", backend=req["backend"], verbose=False,
                              keep_template=True, runtime_configs=rc)
    session = None
    try:
        session = _open(runtime_configs).__enter__()
    except Exception as e1:
        if "security-opt" in str(e1) or "security_opt" in str(e1):
            rc2 = dict(runtime_configs)
            rc2.pop("security_opt", None)
            session = _open(rc2).__enter__()
        else:
            raise
    try:
        r = session.run(program, timeout=req.get("timeout", 60))
    finally:
        try:
            session.__exit__(None, None, None)
        except Exception:
            pass
    out = r.stdout or ""
    marker = "__LLMCTL_RESULT__"
    if marker in out:
        payload = out.rsplit(marker, 1)[1].strip()
        result = base64.b64decode(payload).decode("utf-8", "replace")
        print(json.dumps({"ok": r.exit_code == 0, "result": result,
                          "exit_code": r.exit_code, "stderr": (r.stderr or "")[:4000]}))
    else:
        print(json.dumps({"ok": False, "exit_code": r.exit_code,
                          "error": ((r.stderr or out) or "no output")[:4000],
                          "backend": req["backend"]}))
except Exception as e:
    fail(e)
`

type bridgeInput struct {
	Backend string            `json:"backend"`
	Files   map[string]string `json:"files"`
	Entry   string            `json:"entry"`
	Args    string            `json:"args"`
	Network bool              `json:"network,omitempty"`
	Timeout int               `json:"timeout"`
}

// sandboxBackends returns candidate llm-sandbox backends in preference order.
func sandboxBackends() []string {
	// On Windows the podman Python lib cannot speak named pipes; the docker
	// SDK can (and podman machine's pipe speaks the Docker API). Prefer docker.
	if runtime.GOOS == "windows" {
		return []string{"docker", "podman"}
	}
	var out []string
	if _, err := exec.LookPath("podman"); err == nil {
		out = append(out, "podman")
	}
	if _, err := exec.LookPath("docker"); err == nil {
		out = append(out, "docker")
	}
	if len(out) == 0 {
		out = []string{"podman", "docker"} // let llm-sandbox produce the error
	}
	return out
}

func runSandboxed(ctx context.Context, rec *toolstore.Record, argsJSON string, timeout time.Duration) *Result {
	if rec.Manifest.Runtime != "python" {
		return &Result{Sandbox: true, Error: fmt.Sprintf("sandbox supports python tools only (runtime %q)", rec.Manifest.Runtime)}
	}
	py, err := pythonExe()
	if err != nil {
		return &Result{Sandbox: true, Error: err.Error() + " — llm-sandbox bridge needs a host python"}
	}
	// ship every file in the tool dir (small by convention; entry + helpers)
	files := map[string]string{}
	if err := walkToolFiles(rec.Dir, "", files); err != nil {
		return &Result{Sandbox: true, Error: err.Error()}
	}
	input, _ := json.Marshal(bridgeInput{
		Files: files,
		Entry: filepath.Base(rec.Manifest.Entry),
		Args:  orEmptyArgs(argsJSON),
	})
	_ = input

	var lastErr string
	for _, backend := range sandboxBackends() {
		res := runBridge(ctx, py, backend, files, filepath.Base(rec.Manifest.Entry), orEmptyArgs(argsJSON), rec.Network, timeout)
		if res != nil {
			res.Backend = backend
			if res.OK || !isBackendMissing(res) {
				return res
			}
			lastErr = fmt.Sprintf("%s: %s", backend, res.Error)
			continue
		}
		lastErr = backend + ": bridge failed"
	}
	return &Result{Sandbox: true, Error: "sandbox unavailable — " + lastErr +
		" (install llm-sandbox: python -m pip install 'llm-sandbox[podman,docker]'; start podman machine or Docker)"}
}

func isBackendMissing(res *Result) bool {
	s := strings.ToLower(res.Error + " " + res.Stderr)
	return strings.Contains(s, "missingdependency") || strings.Contains(s, "requires") ||
		strings.Contains(s, "could not find") || strings.Contains(s, "connection") ||
		strings.Contains(s, "filenotfound") || strings.Contains(s, "errorconnect")
}

func runBridge(ctx context.Context, py, backend string, files map[string]string, entry, argsJSON string, network bool, timeout time.Duration) *Result {
	inp, _ := json.Marshal(bridgeInput{
		Backend: backend,
		Files:   files,
		Entry:   entry,
		Args:    argsJSON,
		Network: network,
		Timeout: int(timeout.Seconds()),
	})
	cctx, cancel := context.WithTimeout(ctx, timeout+30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, py, "-c", bridgePy)
	cmd.Stdin = bytes.NewReader(inp)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{buf: &stdout}
	cmd.Stderr = &limitWriter{buf: &stderr}
	err := cmd.Run()
	res := &Result{Sandbox: true}
	if cctx.Err() == context.DeadlineExceeded {
		res.Error = fmt.Sprintf("sandbox run timed out after %s (first run also pulls the sandbox image)", timeout)
		return res
	}
	out := strings.TrimSpace(stdout.String())
	if out != "" {
		var parsed struct {
			OK       bool   `json:"ok"`
			Result   string `json:"result"`
			Stderr   string `json:"stderr"`
			ExitCode int    `json:"exit_code"`
			Error    string `json:"error"`
		}
		if json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &parsed) == nil {
			res.OK = parsed.OK
			res.Result = truncate(parsed.Result, maxOutput)
			res.Stderr = parsed.Stderr
			res.ExitCode = parsed.ExitCode
			res.Error = parsed.Error
			if err != nil && res.Error == "" {
				res.Error = err.Error()
			}
			return res
		}
	}
	res.Error = "sandbox bridge failed"
	if err != nil {
		res.Error += ": " + err.Error()
	}
	if s := strings.TrimSpace(stderr.String()); s != "" {
		res.Stderr = truncate(s, 4000)
	}
	return res
}

// walkToolFiles collects tool files (b64) relative to root, skipping dotfiles.
func walkToolFiles(root, prefix string, out map[string]string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			return nil
		}
		if rel == "." || strings.HasPrefix(filepath.Base(rel), ".") || rel == toolstore.ManifestName {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if len(b) > 4<<20 {
			return fmt.Errorf("tool file %s too large for sandbox shipping", rel)
		}
		out[rel] = base64.StdEncoding.EncodeToString(b)
		return nil
	})
}

// --- helpers ----------------------------------------------------------------

type limitWriter struct {
	buf *bytes.Buffer
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.buf.Len() >= maxOutput {
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

func orEmptyArgs(a string) string {
	if strings.TrimSpace(a) == "" {
		return "{}"
	}
	return a
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…[truncated]"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

var _ = runtime.GOOS
