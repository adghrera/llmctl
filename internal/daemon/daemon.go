// Package daemon wires the gateway, supervisor, and a localhost control
// API into one process. The CLI talks to the control API; long operations
// (installs/downloads) run in the CLI itself and only mutate state.json.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"llmctl/internal/manifest"
	"llmctl/internal/paths"
	"llmctl/internal/proxy"
	"llmctl/internal/store"
	"llmctl/internal/supervisor"
)

// DaemonFile records where the running daemon listens.
type DaemonFile struct {
	GatewayAddr  string `json:"gatewayAddr"`
	ControlAddr  string `json:"controlAddr"`
	PID          int    `json:"pid"`
	StartedAt    time.Time `json:"startedAt"`
}

func daemonFilePath() string { return paths.Home() + string(os.PathSeparator) + "daemon.json" }

// ReadDaemonFile returns the live daemon info, or nil if not running.
func ReadDaemonFile() *DaemonFile {
	b, err := os.ReadFile(daemonFilePath())
	if err != nil {
		return nil
	}
	d := &DaemonFile{}
	if json.Unmarshal(b, d) != nil {
		return nil
	}
	// Stale file check: is the PID alive?
	if !supervisor.ProcessAlive(d.PID) {
		return nil
	}
	return d
}

// Options for running the daemon in the foreground.
type Options struct {
	GatewayAddr string // e.g. ":8080"
	ControlAddr string // e.g. "127.0.0.1:8081"
	APIKeys     []string
	RateRPS     float64
	RateBurst   int
	MaxInflight int
}

// Run starts everything and blocks until SIGINT/SIGTERM.
func Run(ctx context.Context, opts Options) error {
	logger := newLogger()
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	st, err := store.Open()
	if err != nil {
		return err
	}
	reg, err := manifest.Builtin()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	sv := supervisor.New(st, reg, logger)
	go sv.ReapLoop(ctx)

	loadState := func() (*store.State, int64, error) {
		s, err := st.Load()
		if err != nil {
			return nil, 0, err
		}
		var mod int64 = 1
		if fi, err := os.Stat(paths.StateFile()); err == nil {
			mod = fi.ModTime().UnixNano()
		}
		return s, mod, nil
	}
	router := proxy.NewRouter(loadState)

	gw, err := proxy.NewServer(proxy.Config{
		Addr:        opts.GatewayAddr,
		APIKeys:     opts.APIKeys,
		RateRPS:     opts.RateRPS,
		RateBurst:   opts.RateBurst,
		MaxInflight: opts.MaxInflight,
		Logger:      logger,
	}, router)
	if err != nil {
		return err
	}

	control := &controlAPI{st: st, sv: sv, reg: reg, logger: logger, cancel: cancel}
	ctrlSrv := &http.Server{
		Addr:              opts.ControlAddr,
		Handler:           control.mux(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() { errCh <- gw.ListenAndServe(ctx) }()
	go func() {
		ln, err := net.Listen("tcp", opts.ControlAddr)
		if err != nil {
			errCh <- fmt.Errorf("control listen: %w", err)
			return
		}
		logger.Info("control API listening", "addr", opts.ControlAddr)
		errCh <- ctrlSrv.Serve(ln)
	}()

	// Resolve actual ports (for :0) and publish the daemon file.
	gwAddr := resolveAddr(opts.GatewayAddr, 8080)
	ctrlAddr := resolveAddr(opts.ControlAddr, 8081)
	df := &DaemonFile{GatewayAddr: gwAddr, ControlAddr: ctrlAddr, PID: os.Getpid(), StartedAt: time.Now()}
	b, _ := json.Marshal(df)
	os.WriteFile(daemonFilePath(), b, 0o644)
	logger.Info("llmctl daemon up", "gateway", gwAddr, "control", ctrlAddr)

	defer func() {
		os.Remove(daemonFilePath())
		logger.Info("stopping instances...")
		sv.StopAll()
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func resolveAddr(addr string, fallback int) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port != "0" {
		return addr
	}
	return fmt.Sprintf("%s:%d", host, fallback)
}

// ---- control API ----

type controlAPI struct {
	st     *store.Store
	sv     *supervisor.Supervisor
	reg    *manifest.Registry
	logger *slog.Logger
	cancel   func() // cancels the daemon context
}

func (c *controlAPI) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /_control/status", c.status)
	m.HandleFunc("GET /_control/instances", c.listInstances)
	m.HandleFunc("POST /_control/start", c.start)
	m.HandleFunc("POST /_control/stop", c.stop)
	m.HandleFunc("POST /_control/shutdown", c.shutdown)
	m.HandleFunc("GET /_control/catalog", c.catalog)
	return m
}

// shutdown triggers graceful daemon exit (used by `llmctl down`).
func (c *controlAPI) shutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true})
	if c.cancel != nil {
		go func() { time.Sleep(100 * time.Millisecond); c.cancel() }()
	}
}

func (c *controlAPI) status(w http.ResponseWriter, r *http.Request) {
	st, _ := c.st.Load()
	writeJSON(w, 200, map[string]any{
		"ok": true, "backends": len(st.Backends), "models": len(st.Models),
		"instances": len(st.Instances),
	})
}

func (c *controlAPI) catalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, c.reg)
}

func (c *controlAPI) listInstances(w http.ResponseWriter, r *http.Request) {
	st, err := c.st.Load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, st.Instances)
}

func (c *controlAPI) start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instance string            `json:"instance"`
		Model    string            `json:"model"`
		Port     int               `json:"port"`
		Vars     map[string]string `json:"vars"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req) != nil || req.Model == "" {
		http.Error(w, `{"error":"model required"}`, 400)
		return
	}
	id := req.Instance
	if id == "" {
		id = req.Model
	}
	inst, err := c.sv.Start(r.Context(), id, req.Model, req.Port, req.Vars)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, 200, inst)
}

func (c *controlAPI) stop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instance string `json:"instance"`
		Force    bool   `json:"force"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Instance == "" {
		http.Error(w, `{"error":"instance required"}`, 400)
		return
	}
	if err := c.sv.Stop(req.Instance, req.Force); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func newLogger() *slog.Logger {
	f, err := os.OpenFile(paths.DaemonLog(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// ControlClient is used by the CLI to talk to a running daemon.
type ControlClient struct {
	Base string
	hc   *http.Client
}

func NewControlClient() (*ControlClient, error) {
	d := ReadDaemonFile()
	if d == nil {
		return nil, errors.New("daemon not running (start with: llmctl up)")
	}
	return &ControlClient{Base: "http://" + d.ControlAddr, hc: &http.Client{Timeout: 10 * time.Minute}}, nil
}

func (cc *ControlClient) Post(path string, body any) (int, []byte, error) {
	b, _ := json.Marshal(body)
	resp, err := cc.hc.Post(cc.Base+path, "application/json", strings.NewReader(string(b)))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, nil
}

func (cc *ControlClient) Get(path string) (int, []byte, error) {
	resp, err := cc.hc.Get(cc.Base + path)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, nil
}
