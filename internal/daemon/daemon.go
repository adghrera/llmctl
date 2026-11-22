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
	"bytes"
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
	"llmctl/internal/backup"
	"llmctl/internal/cost"
	"llmctl/internal/events"
	"llmctl/internal/jobs"
	"llmctl/internal/paths"
	"llmctl/internal/proxy"
	"llmctl/internal/scheduler"
	"llmctl/internal/store"
	"llmctl/internal/supervisor"
	"llmctl/internal/ui"
	"llmctl/internal/webhook"
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
	go sv.HealthLoop(ctx)
	go sv.AutoStartAll(ctx)

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

	// Read settings to determine if auth is required.
	// Defaults to OFF so a fresh install works without an API key; flip it
	// ON in Settings (or via --api-key) to require Authorization on /v1/*.
	requireAuth := false
	allowCORS := false
	maxBodyMB := 0
	rateRPS := opts.RateRPS
	rateBurst := opts.RateBurst
	s, err := store.Open()
	if err == nil {
		state, loadErr := s.Load()
		if loadErr == nil && state.Settings != nil {
			requireAuth = state.Settings.RequireAuth
			allowCORS = state.Settings.AllowCORS
			maxBodyMB = state.Settings.MaxBodyMB
			if state.Settings.RateRPS > 0 {
				rateRPS = state.Settings.RateRPS // runtime-adjusted default wins (F152)
			}
			if state.Settings.RateBurst > 0 {
				rateBurst = state.Settings.RateBurst
			}
			if err := SetLogLevel(state.Settings.LogLevel); err != nil { // log verbosity (F171)
				logger.Warn("applying log level failed", "error", err)
			}
		}
	}

	gw, err := proxy.NewServer(proxy.Config{
		Addr:        opts.Addr,
		APIKeys:     opts.APIKeys,
		KeyCheck:    nil, // mgmt.KeyCheck set below
		RequireAuth: requireAuth,
		AllowCORS:   allowCORS,
		RateRPS:     rateRPS,
		RateBurst:   rateBurst,
		MaxInflight: opts.MaxInflight,
		// Persist the request log across restarts (F086).
		RequestLogPath: paths.RequestLog(),
		Logger:         logger,
	}, router)
	if err != nil {
		return err
	}
	gw.SetMaxBodyMB(maxBodyMB) // gateway body cap from settings (F121)

	mgmt := api.New(st, reg, sv, jm, logger, gwPort, gw, cancel)
	mgmt.SetApplyLogLevel(SetLogLevel) // runtime log verbosity (F171)
	// Wire the key checker after mgmt is created (circular ref).
	gw.SetKeyCheck(mgmt.KeyCheck)

	// Restore per-key token counters so quotas (F074) survive restarts (F080).
	// Counters key on the key id (F109); legacy entries keyed by the old
	// 8-char prefix are remapped to the matching key's id.
	if cur, err := st.Load(); err == nil && len(cur.KeyUsage) > 0 {
		byPrefix := make(map[string]string)
		for _, k := range cur.APIKeys {
			p := k.Prefix
			if len(p) > 8 {
				p = p[:8]
			}
			if _, dup := byPrefix[p]; !dup {
				byPrefix[p] = k.ID
			}
		}
		seed := make(map[string]proxy.KeyUsage, len(cur.KeyUsage))
		for k, u := range cur.KeyUsage {
			if _, ok := cur.APIKeys[k]; !ok {
				if id, isPrefix := byPrefix[k]; isPrefix {
					k = id
				}
			}
			seed[k] = proxy.KeyUsage{InputTok: u.Input, OutputTok: u.Output, TotalTok: u.Total, Requests: u.Requests, Cost: u.Cost}
		}
		gw.SeedKeyUsage(seed)
	}
	// Restore the daily usage rollup so history survives restarts (F097).
	if cur, err := st.Load(); err == nil && len(cur.DailyUsage) > 0 {
		gw.SeedDaily(cur.DailyUsage)
	}
	// Per-key daily rollup (F126).
	if cur, err := st.Load(); err == nil && len(cur.DailyKeyUsage) > 0 {
		gw.SeedDailyKeys(cur.DailyKeyUsage)
	}
	syncPricing(st, gw) // per-model price overrides (F102)
	go flushKeyUsageLoop(ctx, st, gw, logger)

	// Initialize scheduler and start the scheduler loop.
	sch, err := scheduler.Open()
	if err != nil {
		return err
	}
	go schedulerLoop(ctx, sch, sv, gw, st, logger)
	// Let the API trigger immediate task runs (F084).
	mgmt.SetScheduleRunner(func(rec scheduler.Record) {
		runScheduledTask(ctx, sch, sv, rec, gw, st, logger)
	})

	// Instance-event webhook (settings.notifyWebhook) + events feed (F073, F099).
	evRing := &events.Ring{}
	mgmt.SetEvents(evRing)
	sv.SetNotifier(func(event string, inst *store.InstanceRecord) {
		evRing.Add(events.Event{Kind: event, Instance: inst.ID, Model: inst.ModelID})
		postEventWebhook(ctx, st, event, inst, logger)
	})

	// Start-on-demand (F100): a gateway request for a model that has an
	// auto-start declaration cold-starts it (when the setting is on).
	gw.SetStartHook(func(model string) bool {
		cur, err := st.Load()
		if err != nil || cur.Settings == nil || !cur.Settings.StartOnDemand {
			return false
		}
		name := strings.ToLower(strings.TrimSpace(model))
		if t, ok := cur.Aliases[name]; ok && strings.TrimSpace(t) != "" {
			name = strings.ToLower(strings.TrimSpace(t))
		}
		var spec *store.AutoStartSpec
		for k, s := range cur.AutoStart {
			if strings.EqualFold(k, name) {
				spec = s
				break
			}
		}
		if spec == nil {
			return false
		}
		instName := spec.Name
		if instName == "" {
			instName = spec.ModelID
		}
		if inst, ok := cur.Instances[instName]; ok && inst.Status == store.StatusRunning {
			return false
		}
		logger.Info("start-on-demand: starting instance", "instance", instName, "model", spec.ModelID)
		sctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		if _, err := sv.Start(sctx, instName, spec.ModelID, spec.BackendID, spec.Port, spec.Vars); err != nil {
			logger.Warn("start-on-demand failed", "instance", instName, "err", err)
			return false
		}
		evRing.Add(events.Event{Kind: "instance.started-on-demand", Instance: instName, Model: spec.ModelID})
		return true
	})

	// Auto-stop instances that have served no gateway request for a while.
	go idleUnloadLoop(ctx, st, sv, gw, logger, evRing)

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
		flushUsage(st, gw, logger) // final synchronous flush (F097)
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
//
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

// rotateDaemonLog renames llmctl.log to llmctl.log.1 when it exceeds
// maxDaemonLogBytes at startup (one generation kept). F093.
const maxDaemonLogBytes = 5 << 20 // 5 MiB

func rotateDaemonLog() {
	fi, err := os.Stat(paths.DaemonLog())
	if err != nil || fi.Size() < maxDaemonLogBytes {
		return
	}
	os.Rename(paths.DaemonLog(), paths.DaemonLog()+".1")
}

// daemonLevel lets the log verbosity be changed at runtime via settings (F171).
var daemonLevel slog.LevelVar

func newLogger() *slog.Logger {
	rotateDaemonLog()
	f, err := os.OpenFile(paths.DaemonLog(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: &daemonLevel}))
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: &daemonLevel}))
}

// SetLogLevel changes the daemon's log verbosity at runtime: "" or "info",
// "debug", "warn", "error" (F171).
func SetLogLevel(name string) error {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "info":
		lvl = slog.LevelInfo
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return fmt.Errorf("unknown log level %q (want debug, info, warn or error)", name)
	}
	daemonLevel.Set(lvl)
	return nil
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
	return cc.do(method, path, body, "")
}

// DoKey is Do with an extra Authorization: Bearer <key> header (used by
// endpoints that forward a gateway key, e.g. /api/v1/structured, F279).
func (cc *ControlClient) DoKey(method, path string, body any, key string) (int, []byte, error) {
	return cc.do(method, path, body, key)
}

func (cc *ControlClient) do(method, path string, body any, key string) (int, []byte, error) {
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
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
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

// postEventWebhook POSTs an instance event to settings.notifyWebhook when
// configured (F073). Best-effort: failures are logged only.
func postEventWebhook(ctx context.Context, st *store.Store, event string, inst *store.InstanceRecord, logger *slog.Logger) {
	s, err := st.Load()
	if err != nil || s.Settings == nil || s.Settings.NotifyWebhook == "" {
		return
	}
	// Per-kind filter: empty list forwards everything (F143).
	if len(s.Settings.NotifyEvents) > 0 {
		keep := false
		for _, k := range s.Settings.NotifyEvents {
			if k == event {
				keep = true
				break
			}
		}
		if !keep {
			return
		}
	}
	payload := map[string]any{
		"event":    event,
		"instance": inst.ID,
		"model":    inst.ModelID,
		"time":     time.Now().UTC().Format(time.RFC3339),
	}
	// Async with retries so a flaky receiver never blocks the caller (F163).
	go func(url, secret string) {
		if err := webhook.PostWithRetries(ctx, url, payload, secret, 3); err != nil {
			logger.Error("notify webhook failed", "event", event, "instance", inst.ID, "error", err)
		}
	}(s.Settings.NotifyWebhook, s.Settings.NotifyWebhookSecret)
}

// flushKeyUsageLoop periodically persists per-key token counters and the
// daily usage rollup to the state file so per-key quotas (F074) and usage
// history (F097) survive daemon restarts. Run also calls flushUsage once
// synchronously on shutdown.
func flushKeyUsageLoop(ctx context.Context, st *store.Store, gw *proxy.Server, logger *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			flushUsage(st, gw, logger)
		}
	}
}

// syncPricing pushes per-model price overrides from state into the gateway's
// cost tracker (F102).
func syncPricing(st *store.Store, gw *proxy.Server) {
	cur, err := st.Load()
	if err != nil {
		return
	}
	p := map[string]cost.ModelPricing{}
	for id, m := range cur.Models {
		if m.PriceInPerM != 0 || m.PriceOutPerM != 0 {
			p[id] = cost.ModelPricing{InputPerM: m.PriceInPerM, OutputPerM: m.PriceOutPerM}
		}
	}
	gw.SetPricing(p)
}

// flushUsage writes the gateway's per-key counters and daily rollup to the
// state file (F080, F097).
func flushUsage(st *store.Store, gw *proxy.Server, logger *slog.Logger) {
	syncPricing(st, gw)
	if cur, err := st.Load(); err == nil && cur.Settings != nil {
		gw.PruneDaily(cur.Settings.UsageRetentionDays) // rollup retention (F168)
	}
	usage := gw.KeyUsage()
	daily := gw.DailyUsage()
	dailyKeys := gw.DailyKeyUsage()
	if len(usage) == 0 && len(daily) == 0 {
		return
	}
	if err := st.Update(func(s *store.State) error {
		s.KeyUsage = make(map[string]*store.KeyUsageRecord, len(usage))
		for k, u := range usage {
			s.KeyUsage[k] = &store.KeyUsageRecord{
				Input: u.InputTok, Output: u.OutputTok, Total: u.TotalTok, Requests: u.Requests, Cost: u.Cost, LastUsed: u.LastUsed,
			}
		}
		s.DailyUsage = daily // deep copy from the collector (F097)
		s.DailyKeyUsage = dailyKeys
		return nil
	}); err != nil {
		logger.Error("persist key usage failed", "error", err)
	}
}

// idleUnloadLoop stops running instances that have served no gateway request
// for Settings.IdleUnloadMin minutes (0 = disabled). A tick is skipped while
// any request is in flight so long generations are never cut off (F060).
func idleUnloadLoop(ctx context.Context, st *store.Store, sv *supervisor.Supervisor, gw *proxy.Server, logger *slog.Logger, evRing *events.Ring) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		state, err := st.Load()
		if err != nil || state.Settings == nil {
			continue
		}
		if gw.InFlight() > 0 {
			continue
		}
		var idle time.Duration // 0 = global disabled
		if state.Settings.IdleUnloadMin > 0 {
			idle = time.Duration(state.Settings.IdleUnloadMin) * time.Minute
		}
		now := time.Now()
		for _, id := range state.SortedInstanceIDs() {
			inst := state.Instances[id]
			if inst.Status != store.StatusRunning {
				continue
			}
			// Per-model override: -1 pins the model, >0 replaces the
			// global timeout (which may itself be off); 0 follows the
			// global setting (F136).
			modelIdle := idle
			if mm := state.Models[inst.ModelID]; mm != nil && mm.IdleUnloadMin != 0 {
				if mm.IdleUnloadMin < 0 {
					continue
				}
				modelIdle = time.Duration(mm.IdleUnloadMin) * time.Minute
			}
			if modelIdle <= 0 {
				continue
			}
			last := gw.ModelLastUsed(inst.ModelID)
			if last.IsZero() {
				last = inst.StartedAt
			}
			if now.Sub(last) < modelIdle {
				continue
			}
			logger.Info("idle unload: stopping instance", "instance", id, "model", inst.ModelID,
				"idleFor", now.Sub(last).Round(time.Second), "limitMin", int(modelIdle.Minutes()))
			if err := sv.Stop(id, false); err != nil {
				logger.Error("idle unload: stop failed", "instance", id, "error", err)
			} else {
				evRing.Add(events.Event{Kind: "instance.idle-unloaded", Instance: inst.ID, Model: inst.ModelID})
				postEventWebhook(ctx, st, "instance.idle-unloaded", inst, logger)
			}
		}
	}
}

// schedulerLoop runs scheduled tasks at their configured times.
func schedulerLoop(ctx context.Context, sch *scheduler.Store, sv *supervisor.Supervisor, gw *proxy.Server, st *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			due, err := sch.Due()
			if err != nil {
				logger.Error("scheduler: failed to list due tasks", "error", err)
				continue
			}
			for _, rec := range due {
				go runScheduledTask(ctx, sch, sv, rec, gw, st, logger)
			}
		}
	}
}

// runScheduledTask executes a single scheduled task. Chat tasks make a
// completion request and save the result as a conversation; instance tasks
// start or stop an instance (F076). If the task has a webhook_url, the
// outcome (ok/error) is POSTed there afterwards.
func runScheduledTask(ctx context.Context, sch *scheduler.Store, sv *supervisor.Supervisor, rec scheduler.Record, gw *proxy.Server, st *store.Store, logger *slog.Logger) {
	start := time.Now()
	content, err := doScheduledTask(ctx, sch, sv, rec, logger)
	status := "ok"
	errMsg := ""
	if err != nil {
		status = "error"
		errMsg = err.Error()
		logger.Error("scheduler: task failed", "task", rec.ID, "name", rec.Spec.Name, "error", err)
	} else {
		logger.Info("scheduler: task executed", "task", rec.ID, "name", rec.Spec.Name)
	}
	// Run-history ring for the UI (F145).
	detail := errMsg
	if detail == "" {
		if r := []rune(content); len(r) > 160 {
			detail = string(r[:160]) + "…"
		} else {
			detail = content
		}
	}
	kind := rec.Spec.Kind
	if kind == "" {
		kind = "chat"
	}
	scheduler.LogRun(scheduler.Run{Time: time.Now().UTC(), TaskID: rec.ID, Name: rec.Spec.Name,
		Kind: kind, Status: status, DurationMs: time.Since(start).Milliseconds(), Detail: detail})
	if rec.Spec.WebhookURL != "" {
		secret := ""
		if cur, err := st.Load(); err == nil && cur.Settings != nil {
			secret = cur.Settings.NotifyWebhookSecret
		}
		payload := map[string]any{
			"event":       "llmctl.scheduler.task",
			"task_id":     rec.ID,
			"task_name":   rec.Spec.Name,
			"model":       rec.Spec.Model,
			"status":      status,
			"finished_at": time.Now().UTC().Format(time.RFC3339),
		}
		if status == "ok" {
			payload["content"] = content
		} else {
			payload["error"] = errMsg
		}
		if werr := webhook.PostWithRetries(ctx, rec.Spec.WebhookURL, payload, secret, 3); werr != nil {
			logger.Error("scheduler: webhook failed", "task", rec.ID, "error", werr)
		}
	}
}

// runScheduledBackup writes a state snapshot to $LLMCTL_HOME/backups/ and
// prunes old ones, keeping the newest keep (0 = settings.backupKeep or 7). F077, F181.
func runScheduledBackup(keep int) (string, error) {
	if keep <= 0 {
		keep = defaultBackupKeep()
	}
	name, size, err := backup.CreateSnapshot(paths.BackupsDir(), keep)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("backup %s written (%d bytes), keeping %d", name, size, keep), nil
}

// defaultBackupKeep resolves the snapshot retention: settings.backupKeep when
// set, else 7 (F181).
func defaultBackupKeep() int {
	if st, err := store.Open(); err == nil {
		if s, err := st.Load(); err == nil && s.Settings != nil && s.Settings.BackupKeep > 0 {
			return s.Settings.BackupKeep
		}
	}
	return 7
}

// doScheduledTask runs the chat completion and saves the conversation.
func doScheduledTask(ctx context.Context, sch *scheduler.Store, sv *supervisor.Supervisor, rec scheduler.Record, logger *slog.Logger) (string, error) {
	// Instance tasks: start/stop an instance instead of chatting (F076).
	switch rec.Spec.KindOrDefault() {
	case "instance.start":
		id := rec.Spec.Instance
		if id == "" {
			id = rec.Spec.Model
		}
		inst, err := sv.Start(ctx, id, rec.Spec.Model, "", 0, nil)
		if err != nil {
			return "", fmt.Errorf("start instance %q: %w", id, err)
		}
		return fmt.Sprintf("instance %s started (port %d)", inst.ID, inst.Port), nil
	case "instance.stop":
		id := rec.Spec.Instance
		if id == "" {
			id = rec.Spec.Model
		}
		if err := sv.Stop(id, false); err != nil {
			return "", fmt.Errorf("stop instance %q: %w", id, err)
		}
		return fmt.Sprintf("instance %s stopped", id), nil
	case "backup":
		return runScheduledBackup(rec.Spec.Keep)
	}

	// Build the chat request body.
	body := map[string]any{
		"model":    rec.Spec.Model,
		"messages": []map[string]string{{"role": "user", "content": rec.Spec.Prompt}},
		"stream":   false,
	}
	b, _ := json.Marshal(body)

	// Use the daemon's own address to make the request.
	cc, err := NewControlClient()
	if err != nil {
		return "", fmt.Errorf("cannot create control client: %w", err)
	}

	// Build the request URL.
	req, err := http.NewRequest(http.MethodPost, cc.Base+"/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := cc.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("task execution failed: status %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response choices")
	}

	assistantContent := result.Choices[0].Message.Content

	// Save the conversation.
	chatReq := map[string]any{
		"model": rec.Spec.Model,
		"messages": []map[string]string{
			{"role": "user", "content": rec.Spec.Prompt},
			{"role": "assistant", "content": assistantContent},
		},
	}
	chatB, _ := json.Marshal(chatReq)
	chatReq2, _ := http.NewRequest(http.MethodPost, cc.Base+"/api/v1/chats", bytes.NewReader(chatB))
	chatReq2.Header.Set("Content-Type", "application/json")
	if _, err := cc.hc.Do(chatReq2); err != nil {
		return "", fmt.Errorf("failed to save conversation: %w", err)
	}

	// Update next run time.
	if err := sch.UpdateNextRun(rec.ID); err != nil {
		logger.Error("scheduler: failed to update next run", "task", rec.ID, "error", err)
	}
	return assistantContent, nil
}
