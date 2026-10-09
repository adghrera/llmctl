// Package daemon wires the OpenAI gateway, the management API, the embedded
// UI, and the process supervisor into ONE process and ONE port.
//
//	/v1/*            OpenAI-compatible gateway (auth + rate limit + routing)
//	/healthz         gateway liveness
//	/api/v1/*        management API (backends, models, instances, keys, jobs)
//	/*               embedded web UI
//
// The CLI talks to the management API; long operations (installs, downloads)
// run as tracked jobs inside the daemon and stream progress over SSE.
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

	"llmctl/internal/api"
	"llmctl/internal/backend"
	"llmctl/internal/jobs"
	"llmctl/internal/paths"
	"llmctl/internal/proxy"
	"llmctl/internal/store"
	"llmctl/internal/supervisor"
	"llmctl/internal/ui"
)

// DaemonFile records where the running daemon listens.
type DaemonFile struct {
	Addr      string    `json:"addr"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
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
	if !supervisor.ProcessAlive(d.PID) {
		return nil
	}
	return d
}

// Options for running the daemon.
type Options struct {
	Addr        string   // single address, e.g. ":8080"
	APIKeys     []string // static gateway keys (in addition to stored keys)
	RateRPS     float64
	RateBurst   int
	MaxInflight int
}

// Run starts everything on one port and blocks until SIGINT/SIGTERM.
func Run(ctx context.Context, opts Options) error {
	logger := newLogger()
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	st, err := store.Open()
	if err != nil {
		return err
	}
	reg, err := backend.NewRegistry()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	jm := jobs.NewManager()
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
	router := proxy.NewRouter(loadState, sv.Session)

	// Resolve the actual port (for :0) so the API can report client URLs.
	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", opts.Addr, err)
	}
	gwPort := ln.Addr().(*net.TCPAddr).Port

	mgmt := api.New(st, reg, sv, jm, logger, gwPort, cancel)
	gw, err := proxy.NewServer(proxy.Config{
		Addr:        opts.Addr,
		APIKeys:     opts.APIKeys,
		KeyCheck:    mgmt.KeyCheck,
		RateRPS:     opts.RateRPS,
		RateBurst:   opts.RateBurst,
		MaxInflight: opts.MaxInflight,
		Logger:      logger,
	}, router)
	if err != nil {
		return err
	}

	// One mux: gateway + management API + UI.
	root := http.NewServeMux()
	root.Handle("/api/v1/", mgmt.Handler())
	root.Handle("/v1/", gw.Handler())
	root.Handle("/healthz", gw.Handler())
	root.Handle("/", ui.Handler())

	srv := &http.Server{
		Handler:           root,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	df := &DaemonFile{Addr: dialableAddr(ln, opts.Addr), PID: os.Getpid(), StartedAt: time.Now()}
	b, _ := json.Marshal(df)
	os.WriteFile(daemonFilePath(), b, 0o644)
	logger.Info("llmctl daemon up", "addr", df.Addr)

	defer func() {
		os.Remove(daemonFilePath())
		logger.Info("stopping instances...")
		sv.StopAll()
	}()

	go func() {
		<-ctx.Done()
		ctx2, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		srv.Shutdown(ctx2)
	}()
	err = srv.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// dialableAddr returns an address the CLI can actually connect to. When the
// daemon binds an unspecified host, the raw bind address is not dialable on
// some platforms (notably Windows, where "[::]:port" does not route to
// loopback). We record a loopback address for the same port instead:
//   - IPv6 dual-stack bind ("::")  → "[::1]:port"  (always accepted)
//   - IPv4-only bind ("0.0.0.0")   → "127.0.0.1:port"
// An explicit host (e.g. "127.0.0.1:8080" or "10.0.0.5:8080") is kept as-is.
func dialableAddr(ln net.Listener, requested string) string {
	bind := ln.Addr().String()
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return bind
	}
	switch host {
	case "::":
		return net.JoinHostPort("::1", port)
	case "0.0.0.0", "":
		return net.JoinHostPort("127.0.0.1", port)
	}
	// Preserve the requested host if it was explicit (covers "10.0.0.5:port").
	if h, _, err := net.SplitHostPort(requested); err == nil && h != "" && h != "::" && h != "0.0.0.0" {
		return net.JoinHostPort(h, port)
	}
	return bind
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
	return &ControlClient{Base: "http://" + d.Addr, hc: &http.Client{Timeout: 10 * time.Minute}}, nil
}

func (cc *ControlClient) Do(method, path string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, cc.Base+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cc.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, nil
}

func (cc *ControlClient) Post(path string, body any) (int, []byte, error) {
	return cc.Do(http.MethodPost, path, body)
}

func (cc *ControlClient) Get(path string) (int, []byte, error) {
	return cc.Do(http.MethodGet, path, nil)
}

func (cc *ControlClient) Delete(path string) (int, []byte, error) {
	return cc.Do(http.MethodDelete, path, nil)
}
