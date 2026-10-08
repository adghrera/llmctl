// Package supervisor owns backend child processes: launch, health-wait,
// stop, and reaping zombies (detecting crashed instances).
package supervisor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"llmctl/internal/manifest"
	"llmctl/internal/paths"
	"llmctl/internal/store"
)

type Supervisor struct {
	store   *store.Store
	reg     *manifest.Registry
	logger  *slog.Logger
	httpC   *http.Client
}

func New(st *store.Store, reg *manifest.Registry, logger *slog.Logger) *Supervisor {
	return &Supervisor{
		store:  st,
		reg:    reg,
		logger: logger,
		httpC:  &http.Client{Timeout: 5 * time.Second},
	}
}

// freePort asks the OS for an unused TCP port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// Start launches (or attaches) an instance for modelID and waits for health.
func (sv *Supervisor) Start(ctx context.Context, instanceID, modelID string, port int, vars map[string]string) (*store.InstanceRecord, error) {
	st, err := sv.store.Load()
	if err != nil {
		return nil, err
	}
	mdef, ok := sv.reg.Model(modelID)
	if !ok {
		return nil, fmt.Errorf("unknown model %q (see: llmctl models list)", modelID)
	}
	bdef, ok := sv.reg.Backend(mdef.Backend)
	if !ok {
		return nil, fmt.Errorf("model %q references unknown backend %q", modelID, mdef.Backend)
	}
	brec, ok := st.Backends[bdef.ID]
	if !ok {
		return nil, fmt.Errorf("backend %q not installed (see: llmctl backends install %s)", bdef.ID, bdef.ID)
	}
	mrec, ok := st.Models[mdef.ID]
	if !ok {
		return nil, fmt.Errorf("model %q not downloaded (see: llmctl models install %s)", mdef.ID, mdef.ID)
	}

	if port == 0 {
		if port, err = freePort(); err != nil {
			return nil, err
		}
	}
	if existing := st.Instances[instanceID]; existing != nil && existing.Status == store.StatusRunning {
		return nil, fmt.Errorf("instance %q already running on port %d", instanceID, existing.Port)
	}

	logDir := paths.InstanceLogDir(instanceID)
	os.MkdirAll(logDir, 0o755)

	inst := &store.InstanceRecord{
		ID: instanceID, ModelID: mdef.ID, BackendID: bdef.ID,
		Port: port, Status: "starting", PID: 0, StartedAt: time.Now(),
		Vars: vars, LogDir: logDir, APIBasePath: apiBase(bdef),
	}

	// Attach-mode backends (ollama/custom): no child process, just verify.
	if bdef.Binary.Kind == "path" {
		if err := sv.waitHealthy(ctx, inst, bdef); err != nil {
			return nil, fmt.Errorf("attach failed (is the server running on port %d?): %w", port, err)
		}
		inst.Attached = true
		inst.Status = store.StatusRunning
		if err := sv.put(inst); err != nil {
			return nil, err
		}
		return inst, nil
	}

	if brec.BinaryPath == "" {
		return nil, fmt.Errorf("backend %q has no installed binary", bdef.ID)
	}

	args := buildArgs(bdef, port, mrec.Path, vars)
	cmd := exec.Command(brec.BinaryPath, args...)
	cmd.Dir = filepath.Dir(brec.BinaryPath)
	cmd.Env = append(os.Environ(), bdef.Runtime.EnvExtra...)

	outLog, err := os.OpenFile(filepath.Join(logDir, "stdout.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	errLog, err := os.OpenFile(filepath.Join(logDir, "stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	cmd.Stdout = io.MultiWriter(outLog)
	cmd.Stderr = io.MultiWriter(errLog)
	setSysProc(cmd) // detach process group (unix) / job object flags (windows)

	sv.logger.Info("launching instance", "id", instanceID, "backend", bdef.ID, "port", port, "args", strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn %s: %w", brec.BinaryPath, err)
	}
	inst.PID = cmd.Process.Pid

	// Reap: when the child exits, mark it stopped.
	go func() {
		err := cmd.Wait()
		outLog.Close()
		errLog.Close()
		cur, _ := sv.store.Load()
		if rec := cur.Instances[instanceID]; rec != nil && rec.Status != store.StatusStopped {
			rec.Status = store.StatusStopped
			if err != nil {
				rec.LastError = err.Error()
			}
			sv.store.Update(func(s *store.State) error {
				if r := s.Instances[instanceID]; r != nil {
					r.Status = store.StatusStopped
					r.LastError = rec.LastError
				}
				return nil
			})
			sv.logger.Warn("instance exited", "id", instanceID, "err", err)
		}
	}()

	if err := sv.waitHealthy(ctx, inst, bdef); err != nil {
		sv.kill(inst)
		sv.store.Update(func(s *store.State) error { delete(s.Instances, instanceID); return nil })
		return nil, fmt.Errorf("instance failed health check (logs: %s): %w", logDir, err)
	}
	inst.Status = store.StatusRunning
	if err := sv.put(inst); err != nil {
		return nil, err
	}
	return inst, nil
}

// Stop gracefully stops an owned instance (or forgets an attached one).
func (sv *Supervisor) Stop(instanceID string, force bool) error {
	st, err := sv.store.Load()
	if err != nil {
		return err
	}
	inst, ok := st.Instances[instanceID]
	if !ok {
		return fmt.Errorf("no instance %q", instanceID)
	}
	if inst.Attached {
		return sv.forgetInstance(instanceID)
	}
	if inst.Status != store.StatusRunning {
		return sv.forgetInstance(instanceID)
	}
	proc, err := os.FindProcess(inst.PID)
	if err != nil {
		return sv.forgetInstance(instanceID)
	}
	grace := 10 * time.Second
	sv.store.Update(func(s *store.State) error {
		if r := s.Instances[instanceID]; r != nil {
			r.Status = "stopping"
		}
		return nil
	})
	if !force {
		terminate(proc)
		deadline := time.Now().Add(grace)
		for time.Now().Before(deadline) {
			if !processAlive(inst.PID) {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	if processAlive(inst.PID) {
		proc.Kill()
	}
	return sv.forgetInstance(instanceID)
}

func (sv *Supervisor) forgetInstance(id string) error {
	return sv.store.Update(func(s *store.State) error { delete(s.Instances, id); return nil })
}

// StopAll stops every owned instance (daemon shutdown path).
func (sv *Supervisor) StopAll() {
	st, _ := sv.store.Load()
	if st == nil {
		return
	}
	for _, id := range st.SortedInstanceIDs() {
		sv.Stop(id, false)
	}
}

// reap marks dead-PID instances as stopped (crash detection).
func (sv *Supervisor) reap() {
	st, err := sv.store.Load()
	if err != nil {
		return
	}
	for _, inst := range st.Instances {
		if inst.Attached || inst.Status != store.StatusRunning {
			continue
		}
		if !processAlive(inst.PID) {
			sv.logger.Warn("reaped dead instance", "id", inst.ID, "pid", inst.PID)
			sv.store.Update(func(s *store.State) error {
				if r := s.Instances[inst.ID]; r != nil {
					r.Status = store.StatusStopped
					r.LastError = "process died"
				}
				return nil
			})
		}
	}
}

// ReapLoop runs reap until ctx is done.
func (sv *Supervisor) ReapLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sv.reap()
		}
	}
}

func (sv *Supervisor) put(inst *store.InstanceRecord) error {
	return sv.store.Update(func(s *store.State) error {
		s.Instances[inst.ID] = inst
		return nil
	})
}

func (sv *Supervisor) waitHealthy(ctx context.Context, inst *store.InstanceRecord, bdef *manifest.BackendDef) error {
	timeout := time.Duration(bdef.Runtime.HealthTimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d%s", inst.Port, bdef.Runtime.HealthPath)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %v waiting for %s", timeout, url)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := sv.httpC.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (sv *Supervisor) kill(inst *store.InstanceRecord) {
	if p, err := os.FindProcess(inst.PID); err == nil {
		p.Kill()
	}
}

// ProcessAlive reports whether a PID is running (exported for daemon
// stale-file checks).
func ProcessAlive(pid int) bool { return processAlive(pid) }

// buildArgs assembles the CLI line from the manifest templates.
func buildArgs(bdef *manifest.BackendDef, port int, modelPath string, vars map[string]string) []string {
	vars = mergeVars(bdef, vars)
	var out []string
	if bdef.Runtime.PortFlag != "" {
		out = append(out, splitArgs(expand(bdef.Runtime.PortFlag, port, modelPath, vars))...)
	}
	if bdef.Runtime.ModelFlag != "" {
		out = append(out, splitArgs(expand(bdef.Runtime.ModelFlag, port, modelPath, vars))...)
	}
	if bdef.Runtime.ArgsTemplate != "" {
		out = append(out, splitArgs(expand(bdef.Runtime.ArgsTemplate, port, modelPath, vars))...)
	}
	return out
}

func mergeVars(bdef *manifest.BackendDef, user map[string]string) map[string]string {
	out := map[string]string{}
	for _, v := range bdef.Runtime.Vars {
		out[v.Name] = v.Default
	}
	for k, v := range user {
		out[k] = v
	}
	return out
}

func expand(tmpl string, port int, modelPath string, vars map[string]string) string {
	r := strings.NewReplacer(
		"{port}", strconv.Itoa(port),
		"{model}", modelPath,
		"{model_dir}", modelPath,
	)
	s := r.Replace(tmpl)
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// splitArgs splits respecting double quotes.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for _, ch := range s {
		switch {
		case ch == '"':
			inQ = !inQ
		case ch == ' ' && !inQ:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// apiBase derives the OpenAI-compatible root path for a backend.
func apiBase(bdef *manifest.BackendDef) string {
	if bdef.Runtime.ChatCompletionsPath != "" {
		return strings.TrimSuffix(bdef.Runtime.ChatCompletionsPath, "/chat/completions")
	}
	return "/v1"
}
