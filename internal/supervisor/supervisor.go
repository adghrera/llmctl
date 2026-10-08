// Package supervisor owns backend child processes: launch, health-wait,
// stop, and reaping zombies (detecting crashed instances). It supports three
// instance kinds:
//
//   - http process:  llmctl launches a binary that serves OpenAI HTTP on a
//     free port (llama.cpp, ...). Reverse-proxied by the gateway.
//   - stdio process: llmctl launches a binary speaking llmctl-stdio/1 on
//     stdin/stdout. The live *stdio.Session is held in memory; the gateway
//     translates OpenAI requests over the pipe.
//   - attach:        the user runs the server; llmctl only verifies health
//     and routes to a fixed URL (ollama, vllm, custom, remote).
package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"llmctl/internal/backend"
	"llmctl/internal/paths"
	"llmctl/internal/stdio"
	"llmctl/internal/store"
)

type Supervisor struct {
	store  *store.Store
	reg    *backend.Registry
	logger *slog.Logger
	httpC  *http.Client

	mu       sync.Mutex
	sessions map[string]*stdio.Session // stdio instance id -> live session
}

func New(st *store.Store, reg *backend.Registry, logger *slog.Logger) *Supervisor {
	return &Supervisor{
		store:    st,
		reg:      reg,
		logger:   logger,
		httpC:    &http.Client{Timeout: 5 * time.Second},
		sessions: map[string]*stdio.Session{},
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

// Start launches (or attaches) an instance for modelID using the backend
// named by backendID (or the model's default backend if backendID is empty).
// vars are user parameter overrides. It blocks until the instance is healthy.
func (sv *Supervisor) Start(ctx context.Context, instanceID, modelID, backendID string, port int, vars map[string]string) (*store.InstanceRecord, error) {
	st, err := sv.store.Load()
	if err != nil {
		return nil, err
	}
	mrec, ok := st.Models[modelID]
	if !ok {
		return nil, fmt.Errorf("unknown model %q (see: llmctl models list)", modelID)
	}
	if backendID == "" {
		backendID = mrec.BackendID
	}
	b, ok := sv.reg.Get(backendID)
	if !ok {
		return nil, fmt.Errorf("unknown backend %q (see: llmctl backends list)", backendID)
	}
	if _, ok := st.Backends[backendID]; !ok && b.Kind() != backend.KindAttach {
		return nil, fmt.Errorf("backend %q not installed (see: llmctl backends install %s)", backendID, backendID)
	}

	if existing := st.Instances[instanceID]; existing != nil && existing.Status == store.StatusRunning {
		return nil, fmt.Errorf("instance %q already running", instanceID)
	}

	logDir := paths.InstanceLogDir(instanceID)
	os.MkdirAll(logDir, 0o755)

	spec, err := b.Spec(mrec, vars)
	if err != nil {
		return nil, err
	}

	inst := &store.InstanceRecord{
		ID: instanceID, ModelID: modelID, BackendID: backendID,
		Transport: spec.Transport, Status: "starting",
		StartedAt: time.Now(), Vars: vars, LogDir: logDir,
		APIBasePath: spec.APIBasePath,
	}

	switch spec.Kind {
	case backend.KindAttach:
		return sv.startAttach(ctx, inst, spec)
	case backend.KindProcess:
		if spec.Transport == backend.TransportStdio {
			return sv.startStdio(inst, spec)
		}
		return sv.startHTTP(ctx, inst, spec, port)
	default:
		return nil, fmt.Errorf("unknown kind %q", spec.Kind)
	}
}

// startAttach verifies an externally-run server is reachable.
func (sv *Supervisor) startAttach(ctx context.Context, inst *store.InstanceRecord, spec backend.Spec) (*store.InstanceRecord, error) {
	base := spec.AttachURL
	if base == "" {
		return nil, fmt.Errorf("attach backend %q has no url", inst.BackendID)
	}
	if u, err := url.Parse(base); err == nil {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			inst.Port = p
		}
	}
	inst.Attached = true
	hp := spec.HealthPath
	if hp == "" {
		hp = "/health"
	}
	if err := sv.waitHealthy(ctx, strings.TrimSuffix(base, "/")+hp); err != nil {
		return nil, fmt.Errorf("attach failed (is the server running at %s?): %w", base, err)
	}
	inst.Status = store.StatusRunning
	sv.put(inst)
	return inst, nil
}

// startHTTP launches an OpenAI-HTTP backend on a free port and waits for health.
func (sv *Supervisor) startHTTP(ctx context.Context, inst *store.InstanceRecord, spec backend.Spec, port int) (*store.InstanceRecord, error) {
	if port == 0 {
		var err error
		if port, err = freePort(); err != nil {
			return nil, err
		}
	}
	inst.Port = port
	args := renderPort(spec.Args, port)
	cmd := exec.Command(spec.Binary, args...)
	cmd.Dir = filepath.Dir(spec.Binary)
	cmd.Env = envWith(spec.Env)

	outLog, err := os.OpenFile(filepath.Join(inst.LogDir, "stdout.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	errLog, err := os.OpenFile(filepath.Join(inst.LogDir, "stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	cmd.Stdout = outLog
	cmd.Stderr = errLog
	setSysProc(cmd)

	sv.logger.Info("launching instance", "id", inst.ID, "backend", inst.BackendID, "transport", "http", "port", port, "args", strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn %s: %w", spec.Binary, err)
	}
	inst.PID = cmd.Process.Pid

	go func() {
		werr := cmd.Wait()
		outLog.Close()
		errLog.Close()
		sv.markExited(inst.ID, werr)
	}()

	healthPath := spec.HealthPath
	if healthPath == "" {
		healthPath = "/health"
	}
	if err := sv.waitHealthy(ctx, fmt.Sprintf("http://127.0.0.1:%d%s", port, healthPath)); err != nil {
		sv.kill(inst)
		sv.forgetInstance(inst.ID)
		return nil, fmt.Errorf("instance failed health check (logs: %s): %w", inst.LogDir, err)
	}
	inst.Status = store.StatusRunning
	sv.put(inst)
	return inst, nil
}

// startStdio launches a llmctl-stdio/1 backend and holds the live session.
func (sv *Supervisor) startStdio(inst *store.InstanceRecord, spec backend.Spec) (*store.InstanceRecord, error) {
	ready := time.Duration(spec.StdioReadyTimeoutS) * time.Second
	sess, hello, err := stdio.Launch(stdio.LaunchConfig{
		Binary:       spec.Binary,
		Args:         spec.Args,
		Env:          spec.Env,
		Dir:          filepath.Dir(spec.Binary),
		StderrLog:    filepath.Join(inst.LogDir, "stderr.log"),
		ReadyTimeout: ready,
	})
	if err != nil {
		return nil, fmt.Errorf("stdio backend failed to start (logs: %s): %w", inst.LogDir, err)
	}
	if p := sess.Process(); p != nil {
		inst.PID = p.Pid
	}
	sv.mu.Lock()
	sv.sessions[inst.ID] = sess
	sv.mu.Unlock()

	// Reap: when the backend exits, drop the session and mark stopped.
	go func() {
		<-sess.Closed()
		sv.mu.Lock()
		delete(sv.sessions, inst.ID)
		sv.mu.Unlock()
		sv.markExited(inst.ID, fmt.Errorf("stdio backend exited"))
	}()

	sv.logger.Info("launched stdio instance", "id", inst.ID, "backend", inst.BackendID, "pid", inst.PID, "hello", hello.Name)
	inst.Status = store.StatusRunning
	sv.put(inst)
	return inst, nil
}

// Session returns the live stdio session for an instance (nil for http/attach).
func (sv *Supervisor) Session(instanceID string) *stdio.Session {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.sessions[instanceID]
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

	// stdio: close the session (kills the child).
	if inst.Transport == backend.TransportStdio {
		sv.mu.Lock()
		sess := sv.sessions[instanceID]
		delete(sv.sessions, instanceID)
		sv.mu.Unlock()
		if sess != nil {
			sess.Close()
		}
		return sv.forgetInstance(instanceID)
	}

	// http process: signal, wait grace, then force kill.
	proc, err := os.FindProcess(inst.PID)
	if err != nil {
		return sv.forgetInstance(instanceID)
	}
	sv.store.Update(func(s *store.State) error {
		if r := s.Instances[instanceID]; r != nil {
			r.Status = "stopping"
		}
		return nil
	})
	if !force {
		terminate(proc)
		deadline := time.Now().Add(10 * time.Second)
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

// markExited marks a dead owned instance as stopped (crash detection).
func (sv *Supervisor) markExited(id string, werr error) {
	cur, _ := sv.store.Load()
	if cur == nil {
		return
	}
	rec := cur.Instances[id]
	if rec == nil || rec.Status == store.StatusStopped || rec.Attached {
		return
	}
	msg := ""
	if werr != nil {
		msg = werr.Error()
	}
	sv.store.Update(func(s *store.State) error {
		if r := s.Instances[id]; r != nil {
			r.Status = store.StatusStopped
			if msg != "" {
				r.LastError = msg
			}
		}
		return nil
	})
	sv.logger.Warn("instance exited", "id", id, "err", werr)
}

// reap marks dead-PID instances as stopped (crash detection).
func (sv *Supervisor) reap() {
	st, err := sv.store.Load()
	if err != nil {
		return
	}
	for _, inst := range st.Instances {
		if inst.Attached || inst.Status != store.StatusRunning || inst.PID == 0 {
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

// waitHealthy polls url until it returns <500 or the deadline passes.
func (sv *Supervisor) waitHealthy(ctx context.Context, url string) error {
	deadline := time.Now().Add(240 * time.Second)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", url)
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

// ProcessAlive reports whether a PID is running (exported for daemon checks).
func ProcessAlive(pid int) bool { return processAlive(pid) }

// renderPort substitutes {port} in an arg list.
func renderPort(args []string, port int) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strings.ReplaceAll(a, "{port}", fmt.Sprintf("%d", port))
	}
	return out
}

// envWith merges os env + extra.
func envWith(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
