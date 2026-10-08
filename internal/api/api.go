// Package api is llmctl's management API: a single HTTP surface for
// installing backends, downloading models (incl. HuggingFace), configuring
// and starting/stopping instances, managing API keys, and streaming job
// progress. The embedded UI and the CLI both talk to it.
package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"llmctl/internal/backend"
	"llmctl/internal/downloader"
	"llmctl/internal/jobs"
	"llmctl/internal/paths"
	"llmctl/internal/store"
	"llmctl/internal/supervisor"
)

// Server is the management API.
type Server struct {
	store    *store.Store
	reg      *backend.Registry
	sv       *supervisor.Supervisor
	jobs     *jobs.Manager
	logger   *slog.Logger
	gwPort   int // gateway port, for building client URLs
	shutdownFn func() // triggers graceful daemon exit
}

func New(st *store.Store, reg *backend.Registry, sv *supervisor.Supervisor, jm *jobs.Manager, logger *slog.Logger, gwPort int, shutdown func()) *Server {
	return &Server{store: st, reg: reg, sv: sv, jobs: jm, logger: logger, gwPort: gwPort, shutdownFn: shutdown}
}

// Handler returns the management API mux.
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	// Meta
	m.HandleFunc("GET /api/v1/status", s.status)
	m.HandleFunc("GET /api/v1/health", s.status)
	m.HandleFunc("POST /api/v1/shutdown", s.shutdown)
	// Backends
	m.HandleFunc("GET /api/v1/backends", s.listBackends)
	m.HandleFunc("POST /api/v1/backends/{id}/install", s.installBackend)
	m.HandleFunc("POST /api/v1/backends/{id}/uninstall", s.uninstallBackend)
	m.HandleFunc("POST /api/v1/backends", s.addPlugin)
	m.HandleFunc("DELETE /api/v1/backends/{id}", s.removePlugin)
	// Models
	m.HandleFunc("GET /api/v1/models", s.listModels)
	m.HandleFunc("POST /api/v1/models/install", s.installModel)
	m.HandleFunc("DELETE /api/v1/models/{id}", s.removeModel)
	m.HandleFunc("GET /api/v1/hf/search", s.hfSearch)
	m.HandleFunc("GET /api/v1/hf/files", s.hfFiles)
	// Instances
	m.HandleFunc("GET /api/v1/instances", s.listInstances)
	m.HandleFunc("POST /api/v1/instances", s.startInstance)
	m.HandleFunc("DELETE /api/v1/instances/{id}", s.stopInstance)
	// Jobs
	m.HandleFunc("GET /api/v1/jobs", s.listJobs)
	m.HandleFunc("GET /api/v1/jobs/events", s.jobEvents)
	// Keys
	m.HandleFunc("GET /api/v1/keys", s.listKeys)
	m.HandleFunc("POST /api/v1/keys", s.createKey)
	m.HandleFunc("DELETE /api/v1/keys/{id}", s.revokeKey)
	return s.recoverer(m)
}

// KeyCheck returns a verifier for the gateway: true if token matches any
// stored key hash (constant-time compare of the sha256 digests).
func (s *Server) KeyCheck(token string) bool {
	if token == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	h := hex.EncodeToString(sum[:])
	st, err := s.store.Load()
	if err != nil {
		return false
	}
	for _, k := range st.APIKeys {
		if subtleEqual(k.Hash, h) {
			return true
		}
	}
	return false
}

func subtleEqual(a, b string) bool {
	return len(a) == len(b) && constantTimeEq([]byte(a), []byte(b))
}

// shutdown triggers graceful daemon exit (used by `llmctl down`).
func (s *Server) shutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "msg": "shutting down"})
	if s.shutdownFn != nil {
		go func() { time.Sleep(100 * time.Millisecond); s.shutdownFn() }()
	}
}

// ---- meta ----

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	writeJSON(w, 200, map[string]any{
		"ok": true,
		"version": "0.2.0",
		"backends": len(st.Backends),
		"models": len(st.Models),
		"instances": len(st.Instances),
		"gateway": fmt.Sprintf("http://127.0.0.1:%d", s.gwPort),
	})
}

// ---- backends ----

type backendView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Transport   string `json:"transport"`
	Kind        string `json:"kind"`
	Installed   bool   `json:"installed"`
	Version     string `json:"version,omitempty"`
	Plugin      bool   `json:"plugin"`
}

func (s *Server) listBackends(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	out := []backendView{}
	for _, b := range s.reg.List() {
		v := backendView{
			ID: b.ID(), Name: b.Name(), Description: b.Description(),
			Transport: b.Transport(), Kind: b.Kind(), Plugin: s.reg.IsPlugin(b.ID()),
		}
		if rec, ok := st.Backends[b.ID()]; ok {
			v.Installed = true
			v.Version = rec.Version
		}
		out = append(out, v)
	}
	writeJSON(w, 200, out)
}

func (s *Server) installBackend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.reg.Get(id)
	if !ok {
		writeErr(w, 404, "unknown backend: "+id)
		return
	}
	hint := r.URL.Query().Get("asset")
	job := s.jobs.Submit("backend-install", id, func(ctx context.Context, j *jobs.Job) error {
		version, err := b.Install(ctx, hint, func(msg string) {
			j.SetProgress(j.Progress, msg)
		})
		if err != nil {
			return err
		}
		return s.store.Update(func(st *store.State) error {
			st.Backends[id] = &store.BackendRecord{ID: id, Version: version, InstalledAt: time.Now()}
			return nil
		})
	})
	writeJSON(w, 202, map[string]any{"job": job.ID, "status": "running"})
}

func (s *Server) uninstallBackend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, ok := s.reg.Get(id)
	if !ok {
		writeErr(w, 404, "unknown backend: "+id)
		return
	}
	// Refuse if any instance still uses it.
	st, _ := s.store.Load()
	for _, inst := range st.Instances {
		if inst.BackendID == id && inst.Status == store.StatusRunning {
			writeErr(w, 409, "stop instances using "+id+" first")
			return
		}
	}
	if err := b.Uninstall(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.store.Update(func(st *store.State) error { delete(st.Backends, id); return nil })
	writeJSON(w, 200, map[string]any{"ok": true})
}

// addPlugin registers a new plugin backend (JSON body = PluginMeta).
func (s *Server) addPlugin(w http.ResponseWriter, r *http.Request) {
	var m backend.PluginMeta
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&m); err != nil {
		writeErr(w, 400, "invalid plugin JSON: "+err.Error())
		return
	}
	if err := s.reg.AddPlugin(&m); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": m.ID})
}

func (s *Server) removePlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.reg.IsPlugin(id) {
		writeErr(w, 400, "not a plugin (built-in backends can't be removed): "+id)
		return
	}
	if err := s.reg.RemovePlugin(id); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- models ----

type modelView struct {
	ID         string   `json:"id"`
	BackendID  string   `json:"backendId"`
	Path       string   `json:"path"`
	SizeBytes  int64    `json:"sizeBytes"`
	Running    bool     `json:"running"`
	Instances  []string `json:"instances,omitempty"`
	Downloaded bool     `json:"downloaded"`
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	out := []modelView{}
	for id, m := range st.Models {
		v := modelView{ID: id, BackendID: m.BackendID, Path: m.Path, SizeBytes: m.SizeBytes, Downloaded: true}
		for _, inst := range st.Instances {
			if inst.ModelID == id {
				v.Instances = append(v.Instances, inst.ID)
				if inst.Status == store.StatusRunning {
					v.Running = true
				}
			}
		}
		out = append(out, v)
	}
	writeJSON(w, 200, out)
}

type installModelReq struct {
	ID       string `json:"id"`       // model id (defaults to repo/name)
	Backend  string `json:"backend"`  // backend id (default llama.cpp)
	Repo     string `json:"repo"`     // HF repo or direct URL
	Revision string `json:"revision"` // HF revision (default main)
	File     string `json:"file"`     // filename within repo
}

func (s *Server) installModel(w http.ResponseWriter, r *http.Request) {
	var req installModelReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || req.Repo == "" {
		writeErr(w, 400, "repo required (HF repo like owner/name, or a direct URL)")
		return
	}
	id := req.ID
	if id == "" {
		id = sanitizeID(req.Repo + "/" + req.File)
	}
	backendID := req.Backend
	if backendID == "" {
		backendID = "llama.cpp"
	}
	if _, ok := s.reg.Get(backendID); !ok {
		writeErr(w, 400, "unknown backend: "+backendID)
		return
	}
	// Resolve the download URL.
	url := req.Repo
	if !strings.HasPrefix(req.Repo, "http") {
		url = downloader.ResolveHFURL(req.Repo, req.Revision, req.File)
	}
	dest := filepath.Join(paths.ModelDir(id), filepath.Base(req.File))
	job := s.jobs.Submit("model-install", id, func(ctx context.Context, j *jobs.Job) error {
		_, err := downloader.DownloadURL(ctx, url, dest, func(p downloader.Progress) {
			var pct float64
			if p.Total > 0 {
				pct = float64(p.Bytes) / float64(p.Total)
			}
			msg := ""
			if p.SpeedBPS > 0 {
				msg = fmt.Sprintf("%.1f/%.1f MB @ %.0f MB/s",
					float64(p.Bytes)/1e6, float64(p.Total)/1e6, p.SpeedBPS/1e6)
			}
			j.SetProgress(pct, msg)
		})
		if err != nil {
			return err
		}
		fi, _ := os.Stat(dest)
		var size int64
		if fi != nil {
			size = fi.Size()
		}
		return s.store.Update(func(st *store.State) error {
			st.Models[id] = &store.ModelRecord{
				ID: id, BackendID: backendID, Path: dest,
				SizeBytes: size, DownloadedAt: time.Now(),
			}
			return nil
		})
	})
	writeJSON(w, 202, map[string]any{"job": job.ID, "model": id})
}

func (s *Server) removeModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st, _ := s.store.Load()
	m, ok := st.Models[id]
	if !ok {
		writeErr(w, 404, "unknown model: "+id)
		return
	}
	for _, inst := range st.Instances {
		if inst.ModelID == id && inst.Status == store.StatusRunning {
			writeErr(w, 409, "stop instances using this model first")
			return
		}
	}
	if m.Path != "" {
		os.Remove(m.Path)
	}
	s.store.Update(func(st *store.State) error { delete(st.Models, id); return nil })
	writeJSON(w, 200, map[string]any{"ok": true})
}

// hfSearch proxies the HuggingFace model search API (so the browser never
// needs to talk to HF directly, and CORS is a non-issue).
func (s *Server) hfSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "20"
	}
	u := fmt.Sprintf("https://huggingface.co/api/models?search=%s&limit=%s&sort=downloads", urlQueryEscape(q), limit)
	resp, err := http.Get(u)
	if err != nil {
		writeErr(w, 502, "HF search: "+err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, io.LimitReader(resp.Body, 4<<20))
}

// hfFiles lists GGUF files in a HF repo.
func (s *Server) hfFiles(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	revision := r.URL.Query().Get("revision")
	if repo == "" {
		writeErr(w, 400, "repo required")
		return
	}
	entries, err := downloader.HFCatalog(repo, revision)
	if err != nil {
		writeErr(w, 502, "HF catalog: "+err.Error())
		return
	}
	writeJSON(w, 200, entries)
}

// ---- instances ----

type instanceView struct {
	ID        string            `json:"id"`
	ModelID   string            `json:"modelId"`
	BackendID string            `json:"backendId"`
	Transport string            `json:"transport"`
	Port      int               `json:"port"`
	Status    string            `json:"status"`
	Attached  bool              `json:"attached"`
	PID       int               `json:"pid"`
	StartedAt time.Time         `json:"startedAt"`
	Vars      map[string]string `json:"vars,omitempty"`
	LastError string            `json:"lastError,omitempty"`
}

func (s *Server) listInstances(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	out := []instanceView{}
	for _, id := range st.SortedInstanceIDs() {
		i := st.Instances[id]
		out = append(out, instanceView{
			ID: i.ID, ModelID: i.ModelID, BackendID: i.BackendID,
			Transport: i.Transport, Port: i.Port, Status: i.Status,
			Attached: i.Attached, PID: i.PID, StartedAt: i.StartedAt,
			Vars: i.Vars, LastError: i.LastError,
		})
	}
	writeJSON(w, 200, out)
}

type startInstanceReq struct {
	Instance  string            `json:"instance"`
	Model     string            `json:"model"`
	Backend   string            `json:"backend"`
	Port      int               `json:"port"`
	Vars      map[string]string `json:"vars"`
}

func (s *Server) startInstance(w http.ResponseWriter, r *http.Request) {
	var req startInstanceReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || req.Model == "" {
		writeErr(w, 400, "model required")
		return
	}
	id := req.Instance
	if id == "" {
		id = req.Model
	}
	// Start can be slow (model load). Run in a job so the UI can show progress,
	// but also return the instance once ready. We block here (bounded by the
	// health timeout) — the UI opens the request with a long timeout.
	inst, err := s.sv.Start(r.Context(), id, req.Model, req.Backend, req.Port, req.Vars)
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, inst)
}

func (s *Server) stopInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	force := r.URL.Query().Get("force") == "true"
	if err := s.sv.Stop(id, force); err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- jobs ----

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.jobs.List())
}

// jobEvents is an SSE stream of job progress.
func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	// Send current jobs as an initial snapshot.
	for _, j := range s.jobs.List() {
		fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", mustJSON(j))
	}
	flusher.Flush()
	sub := s.jobs.Subscribe()
	// Heartbeat every 15s so proxies don't drop the connection.
	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-hb.C:
			fmt.Fprint(w, ": hb\n\n")
			flusher.Flush()
		case ev := <-sub:
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, mustJSON(ev.Job))
			flusher.Flush()
		}
	}
}

// ---- keys ----

type keyView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	out := []keyView{}
	for _, k := range st.APIKeys {
		out = append(out, keyView{ID: k.ID, Name: k.Name, Prefix: k.Prefix, CreatedAt: k.CreatedAt})
	}
	writeJSON(w, 200, out)
}

type createKeyReq struct {
	Name string `json:"name"`
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var req createKeyReq
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<4)).Decode(&req)
	plaintext := generateKey()
	sum := sha256.Sum256([]byte(plaintext))
	id := newID()
	prefix := plaintext
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	s.store.Update(func(st *store.State) error {
		st.APIKeys[id] = &store.APIKeyRecord{
			ID: id, Name: req.Name, Hash: hex.EncodeToString(sum[:]),
			Prefix: prefix, CreatedAt: time.Now(),
		}
		return nil
	})
	// Return the plaintext exactly once.
	writeJSON(w, 201, map[string]any{
		"id": id, "name": req.Name, "key": plaintext, "prefix": prefix,
		"note": "store this key now; it is shown only once",
	})
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.store.Update(func(st *store.State) error { delete(st.APIKeys, id); return nil })
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- helpers ----

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic in api handler", "panic", rec, "path", r.URL.Path)
				writeErr(w, 500, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func generateKey() string {
	b := make([]byte, 20)
	rand.Read(b)
	return "sk-llm-" + hex.EncodeToString(b)
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sanitizeID(s string) string {
	s = strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "-").Replace(s)
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func urlQueryEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", "+"), "&", "%26")
}

// constantTimeEq is a constant-time byte comparison.
func constantTimeEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
