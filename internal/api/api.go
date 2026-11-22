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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"llmctl/internal/agent"
	"llmctl/internal/backend"
	"llmctl/internal/chatstore"
	"llmctl/internal/cost"
	"llmctl/internal/downloader"
	"llmctl/internal/events"
	"llmctl/internal/files"
	"llmctl/internal/gguf"
	"llmctl/internal/jobs"
	"llmctl/internal/mcp"
	"llmctl/internal/memory"
	"llmctl/internal/paths"
	"llmctl/internal/proxy"
	"llmctl/internal/rag"
	"llmctl/internal/scheduler"
	"llmctl/internal/skills"
	"llmctl/internal/store"
	"llmctl/internal/supervisor"
	"llmctl/internal/toolstore"
	"llmctl/internal/webhook"
	"llmctl/internal/websearch"
)

// Server is the management API.
type Server struct {
	store      *store.Store
	reg        *backend.Registry
	sv         *supervisor.Supervisor
	jobs       *jobs.Manager
	logger     *slog.Logger
	gwPort     int                    // gateway port, for building client URLs
	gw         *proxy.Server          // gateway handle, for runtime setting changes
	searcher   *websearch.Searcher    // keyless web search (F021)
	agentTool  agent.Tool             // web_search tool override (tests); nil = WebSearchTool{searcher}
	chats      *chatstore.Store       // persisted chat conversations (F025)
	tools      *toolstore.Store       // installed agent tools (F033)
	skills     *skills.Store          // user-defined skill templates (F036)
	memory     *memory.Store          // persistent agent memory entries (F040)
	files      *files.Store           // user-uploaded files (F044)
	cost       *cost.Tracker          // token usage and cost tracking (F045)
	sched      *scheduler.Store       // scheduled agent tasks (F046)
	schedRun   func(scheduler.Record) // daemon-provided "run this task now" hook (F084)
	events     *events.Ring           // recent daemon events feed (F099)
	rag        *rag.Store             // knowledge bases for RAG (F047)
	mcpSt      *mcp.Store             // MCP server configs (F054)
	mcpReg     *mcp.Registry          // live MCP connections (F054)
	approvals  *pendingApprovals      // tool calls awaiting user approval (F055)
	shutdownFn func()                 // triggers graceful daemon exit
}

func New(st *store.Store, reg *backend.Registry, sv *supervisor.Supervisor, jm *jobs.Manager, logger *slog.Logger, gwPort int, gw *proxy.Server, shutdown func()) *Server {
	cs, _ := chatstore.Open() // nil-safe: handlers report 503 if unavailable
	return &Server{store: st, reg: reg, sv: sv, jobs: jm, logger: logger, gwPort: gwPort, gw: gw, searcher: websearch.New(), chats: cs, approvals: newPendingApprovals(), shutdownFn: shutdown}
}

// Handler returns the management API mux.
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	// Meta
	m.HandleFunc("GET /api/v1/status", s.status)
	m.HandleFunc("GET /api/v1/health", s.status)
	m.HandleFunc("GET /api/v1/doctor", s.doctor)
	m.HandleFunc("POST /api/v1/shutdown", s.shutdown)
	// Backends
	m.HandleFunc("GET /api/v1/backends", s.listBackends)
	m.HandleFunc("POST /api/v1/backends/{id}/install", s.installBackend)
	m.HandleFunc("POST /api/v1/backends/{id}/uninstall", s.uninstallBackend)
	m.HandleFunc("POST /api/v1/backends", s.addPlugin)
	m.HandleFunc("DELETE /api/v1/backends/{id}", s.removePlugin)
	// Models
	m.HandleFunc("GET /api/v1/models", s.listModels)
	m.HandleFunc("POST /api/v1/models", s.registerModel)
	m.HandleFunc("POST /api/v1/models/install", s.installModel)
	m.HandleFunc("GET /api/v1/catalog", s.listCatalog)
	m.HandleFunc("POST /api/v1/catalog/install", s.installCatalogModel)
	m.HandleFunc("DELETE /api/v1/models/{id}", s.removeModel)
	m.HandleFunc("PUT /api/v1/models/{id}/ctx", s.setModelCtx)
	m.HandleFunc("GET /api/v1/models/{id}/metadata", s.modelMetadata)
	m.HandleFunc("GET /api/v1/hf/search", s.hfSearch)
	m.HandleFunc("GET /api/v1/hf/files", s.hfFiles)
	// Instances
	m.HandleFunc("GET /api/v1/instances", s.listInstances)
	m.HandleFunc("POST /api/v1/instances", s.startInstance)
	m.HandleFunc("DELETE /api/v1/instances/{id}", s.stopInstance)
	m.HandleFunc("POST /api/v1/instances/stop-all", s.stopAllInstances)
	m.HandleFunc("POST /api/v1/instances/{id}/restart", s.restartInstance)
	m.HandleFunc("POST /api/v1/instances/{id}/autorestart", s.setAutoRestart)
	m.HandleFunc("PATCH /api/v1/instances/{id}/note", s.setNote)
	m.HandleFunc("PATCH /api/v1/instances/{id}/maxparallel", s.setMaxParallel)
	m.HandleFunc("PATCH /api/v1/models/{id}/price", s.setModelPrice)
	m.HandleFunc("PATCH /api/v1/models/{id}/idle", s.setModelIdle)
	m.HandleFunc("PUT /api/v1/instances/{id}/vars", s.setInstanceVars)
	m.HandleFunc("GET /api/v1/instances/{id}/logs", s.instanceLogs)
	m.HandleFunc("GET /api/v1/instances/{id}/logs/download", s.instanceLogsDownload)
	// Auto-start specs
	m.HandleFunc("GET /api/v1/autostart", s.listAutoStart)
	m.HandleFunc("GET /api/v1/presets", s.listPresets)
	m.HandleFunc("GET /api/v1/aliases", s.listAliases)
	m.HandleFunc("POST /api/v1/aliases", s.createAlias)
	m.HandleFunc("DELETE /api/v1/aliases/{alias}", s.deleteAlias)
	m.HandleFunc("GET /api/v1/tools", s.listTools)
	m.HandleFunc("POST /api/v1/tools/install", s.installTool)
	m.HandleFunc("POST /api/v1/tools/{id}/enabled", s.setToolEnabled)
	m.HandleFunc("POST /api/v1/tools/{id}/sandbox", s.setToolSandbox)
	m.HandleFunc("POST /api/v1/tools/{id}/run", s.runTool)
	m.HandleFunc("DELETE /api/v1/tools/{id}", s.deleteTool)
	// Skills (F036)
	m.HandleFunc("GET /api/v1/skills", s.listSkills)
	m.HandleFunc("POST /api/v1/skills", s.createSkill)
	m.HandleFunc("GET /api/v1/skills/{id}", s.getSkill)
	m.HandleFunc("PUT /api/v1/skills/{id}", s.updateSkill)
	m.HandleFunc("DELETE /api/v1/skills/{id}", s.deleteSkill)
	m.HandleFunc("POST /api/v1/skills/{id}/enabled", s.setSkillEnabled)
	// Memory (F040)
	m.HandleFunc("GET /api/v1/memory", s.listMemory)
	m.HandleFunc("POST /api/v1/memory", s.createMemory)
	m.HandleFunc("GET /api/v1/memory/{id}", s.getMemory)
	m.HandleFunc("PUT /api/v1/memory/{id}", s.updateMemory)
	m.HandleFunc("DELETE /api/v1/memory/{id}", s.deleteMemory)
	m.HandleFunc("GET /api/v1/memory/search", s.searchMemory)
	// Files (F044)
	m.HandleFunc("GET /api/v1/files", s.listFiles)
	m.HandleFunc("POST /api/v1/files", s.uploadFile)
	m.HandleFunc("GET /api/v1/files/{id}/content", s.getFileContent)
	m.HandleFunc("DELETE /api/v1/files/{id}", s.deleteFile)
	// Knowledge bases (F047)
	m.HandleFunc("GET /api/v1/knowledge-bases", s.listKBs)
	m.HandleFunc("POST /api/v1/knowledge-bases", s.createKB)
	m.HandleFunc("GET /api/v1/knowledge-bases/{id}", s.getKB)
	m.HandleFunc("PUT /api/v1/knowledge-bases/{id}", s.updateKB)
	m.HandleFunc("DELETE /api/v1/knowledge-bases/{id}", s.deleteKB)
	m.HandleFunc("GET /api/v1/knowledge-bases/{id}/documents", s.listKBDocs)
	m.HandleFunc("POST /api/v1/knowledge-bases/{id}/documents", s.uploadKBDoc)
	m.HandleFunc("GET /api/v1/knowledge-bases/{id}/documents/{doc_id}/content", s.getKBDocContent)
	m.HandleFunc("DELETE /api/v1/knowledge-bases/{id}/documents/{doc_id}", s.deleteKBDoc)
	m.HandleFunc("POST /api/v1/knowledge-bases/{id}/search", s.searchKBDocs)
	m.HandleFunc("PUT /api/v1/knowledge-bases/{id}/embed", s.setKBEmbed)
	// Cost (F045)
	m.HandleFunc("GET /api/v1/cost", s.costSummary)
	m.HandleFunc("GET /api/v1/cost/keys", s.costKeys)
	m.HandleFunc("GET /api/v1/cost/models", s.costModels)
	// Scheduler (F046)
	m.HandleFunc("GET /api/v1/scheduler", s.listSchedules)
	m.HandleFunc("GET /api/v1/scheduler/runs", s.schedulerRuns)
	m.HandleFunc("POST /api/v1/scheduler", s.createSchedule)
	m.HandleFunc("GET /api/v1/scheduler/{id}", s.getSchedule)
	m.HandleFunc("PUT /api/v1/scheduler/{id}", s.updateSchedule)
	m.HandleFunc("DELETE /api/v1/scheduler/{id}", s.deleteSchedule)
	m.HandleFunc("POST /api/v1/scheduler/{id}/enabled", s.setScheduleEnabled)
	m.HandleFunc("POST /api/v1/scheduler/{id}/run", s.runScheduleNow)
	// Prompt templates (F043)
	m.HandleFunc("GET /api/v1/prompts", s.listPrompts)
	m.HandleFunc("GET /api/v1/prompts/categories", s.categoriesPrompt)
	m.HandleFunc("GET /api/v1/prompts/{id}", s.findPrompt)
	m.HandleFunc("POST /api/v1/presets", s.savePreset)
	m.HandleFunc("DELETE /api/v1/presets/{name}", s.deletePreset)
	m.HandleFunc("POST /api/v1/presets/{name}/start", s.startPreset)
	m.HandleFunc("POST /api/v1/autostart", s.setAutoStart)
	m.HandleFunc("DELETE /api/v1/autostart/{model}", s.clearAutoStart)
	// Jobs
	m.HandleFunc("GET /api/v1/jobs", s.listJobs)
	m.HandleFunc("GET /api/v1/jobs/events", s.jobEvents)
	// Metrics
	m.HandleFunc("GET /api/v1/metrics", s.metricsSnapshot)
	m.HandleFunc("GET /api/v1/usage/daily", s.dailyUsage)
	m.HandleFunc("GET /api/v1/usage/keys", s.dailyKeyUsage)
	m.HandleFunc("GET /api/v1/events", s.listEvents)
	m.HandleFunc("GET /api/v1/gpu", s.gpuInfo)
	m.HandleFunc("GET /api/v1/disk", s.diskUsage)
	m.HandleFunc("GET /api/v1/backups", s.listBackups)
	m.HandleFunc("POST /api/v1/backups", s.createBackupNow)
	m.HandleFunc("GET /api/v1/backups/{name}", s.downloadBackupFile)
	m.HandleFunc("DELETE /api/v1/backups/{name}", s.deleteBackupFile)
	// MCP servers (F054)
	m.HandleFunc("GET /api/v1/mcp", s.listMCPServers)
	m.HandleFunc("POST /api/v1/mcp", s.createMCPServer)
	m.HandleFunc("DELETE /api/v1/mcp/{id}", s.deleteMCPServer)
	m.HandleFunc("POST /api/v1/mcp/{id}/enabled", s.setMCPEnabled)
	m.HandleFunc("POST /api/v1/mcp/{id}/test", s.testMCPServer)
	m.HandleFunc("GET /api/v1/backup", s.downloadBackup)
	m.HandleFunc("POST /api/v1/restore", s.restoreBackup)
	m.HandleFunc("POST /api/v1/agent/approve", s.approveToolCall)
	m.HandleFunc("POST /api/v1/structured", s.structured)
	m.HandleFunc("POST /api/v1/batch", s.batch)
	m.HandleFunc("GET /api/v1/requests", s.recentRequests)
	m.HandleFunc("GET /api/v1/requests/{id}", s.requestByID)
	// Web search (keyless)
	m.HandleFunc("GET /api/v1/search", s.searchHandler)
	m.HandleFunc("GET /api/v1/chats", s.listChats)
	m.HandleFunc("POST /api/v1/chats", s.saveChat)
	m.HandleFunc("GET /api/v1/chats/search", s.searchChats)
	m.HandleFunc("GET /api/v1/chats/{id}", s.getChat)
	m.HandleFunc("PUT /api/v1/chats/{id}", s.saveChat)
	m.HandleFunc("DELETE /api/v1/chats/{id}", s.deleteChat)
	m.HandleFunc("POST /api/v1/chats/{id}/fork", s.forkChat)
	m.HandleFunc("GET /api/v1/chats/{id}/export", s.exportChat)
	// Agent chat (SSE, tool-using)
	m.HandleFunc("POST /api/v1/chat/agent", s.chatAgent)
	// Keys
	m.HandleFunc("GET /api/v1/keys", s.listKeys)
	m.HandleFunc("POST /api/v1/keys", s.createKey)
	m.HandleFunc("PATCH /api/v1/keys/{id}", s.updateKey)
	m.HandleFunc("POST /api/v1/keys/{id}/reset-usage", s.resetKeyUsage)
	m.HandleFunc("DELETE /api/v1/keys/{id}", s.revokeKey)
	// Settings
	m.HandleFunc("GET /api/v1/settings", s.getSettings)
	m.HandleFunc("POST /api/v1/settings/notify-test", s.testNotifyWebhook)
	m.HandleFunc("GET /api/v1/webhook/log", s.webhookLog)
	m.HandleFunc("PATCH /api/v1/settings", s.updateSettings)
	return s.recoverer(m)
}

// KeyCheck returns the API key record if the token is valid, otherwise nil.
func (s *Server) KeyCheck(token string) *store.APIKeyRecord {
	if token == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(token))
	h := hex.EncodeToString(sum[:])
	st, err := s.store.Load()
	if err != nil {
		return nil
	}
	for _, k := range st.APIKeys {
		if subtleEqual(k.Hash, h) {
			return k
		}
	}
	return nil
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
		"ok":        true,
		"version":   "0.2.0",
		"backends":  len(st.Backends),
		"models":    len(st.Models),
		"instances": len(st.Instances),
		"gateway":   fmt.Sprintf("http://127.0.0.1:%d", s.gwPort),
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
	CtxSize    int      `json:"ctxSize,omitempty"`
	PriceIn    float64  `json:"priceInPerM,omitempty"`  // USD / 1M input tokens (F102)
	PriceOut   float64  `json:"priceOutPerM,omitempty"` // USD / 1M output tokens (F102)
	// IdleUnloadMin: 0 = global, -1 = pinned, >0 = custom minutes (F136).
	IdleUnloadMin int `json:"idleUnloadMin,omitempty"`
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	out := []modelView{}
	for id, m := range st.Models {
		v := modelView{ID: id, BackendID: m.BackendID, Path: m.Path, SizeBytes: m.SizeBytes, Downloaded: true, CtxSize: m.CtxSize, PriceIn: m.PriceInPerM, PriceOut: m.PriceOutPerM, IdleUnloadMin: m.IdleUnloadMin}
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

type registerModelReq struct {
	ID      string `json:"id"`
	Backend string `json:"backend"` // default llama.cpp
	Path    string `json:"path"`    // existing local .gguf file
	CtxSize int    `json:"ctxSize"` // optional default context window
}

var modelIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)

// registerModel handles POST /api/v1/models — register an existing local
// model file (no download) so it can be started like an installed one (F083).
func (s *Server) registerModel(w http.ResponseWriter, r *http.Request) {
	var req registerModelReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	if !modelIDRe.MatchString(req.ID) {
		writeErr(w, 400, "id must match ^[a-z0-9][a-z0-9_-]{0,79}$")
		return
	}
	backendID := req.Backend
	if backendID == "" {
		backendID = "llama.cpp"
	}
	if _, ok := s.reg.Get(backendID); !ok {
		writeErr(w, 400, "unknown backend: "+backendID)
		return
	}
	fi, err := os.Stat(req.Path)
	if err != nil || fi.IsDir() {
		writeErr(w, 400, "model file not readable: "+req.Path)
		return
	}
	st, _ := s.store.Load()
	if _, exists := st.Models[req.ID]; exists {
		writeErr(w, 409, "model already registered: "+req.ID)
		return
	}
	s.store.Update(func(st *store.State) error {
		st.Models[req.ID] = &store.ModelRecord{
			ID:           req.ID,
			BackendID:    backendID,
			Path:         req.Path,
			SizeBytes:    fi.Size(),
			DownloadedAt: time.Now(),
			CtxSize:      req.CtxSize,
		}
		return nil
	})
	writeJSON(w, 201, modelView{
		ID: req.ID, BackendID: backendID, Path: req.Path,
		SizeBytes: fi.Size(), Downloaded: true, CtxSize: req.CtxSize,
	})
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

// setModelCtx sets the default context window (tokens) for a model.
// Body: {"ctxSize": 8192}. ctxSize 0 clears the override (backend default).
// Takes effect on the next start of an instance of this model.
func (s *Server) setModelCtx(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		CtxSize *int `json:"ctxSize"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CtxSize == nil {
		writeErr(w, 400, "expected {\"ctxSize\": <tokens>}")
		return
	}
	if *req.CtxSize < 0 || *req.CtxSize > 1_000_000 {
		writeErr(w, 400, "ctxSize must be between 0 and 1000000 (0 = backend default)")
		return
	}
	st, _ := s.store.Load()
	if _, ok := st.Models[id]; !ok {
		writeErr(w, 404, "unknown model: "+id)
		return
	}
	if err := s.store.Update(func(st *store.State) error {
		st.Models[id].CtxSize = *req.CtxSize
		return nil
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	running := false
	st, _ = s.store.Load()
	for _, inst := range st.Instances {
		if inst.ModelID == id && inst.Status == store.StatusRunning {
			running = true
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "model": id, "ctxSize": *req.CtxSize,
		"restartRequired": running})
}

// modelMetadata handles GET /api/v1/models/{id}/metadata — parses the GGUF
// header of the model file and returns a curated summary plus all KV metadata.
func (s *Server) modelMetadata(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st, _ := s.store.Load()
	m, ok := st.Models[id]
	if !ok {
		writeErr(w, 404, "unknown model: "+id)
		return
	}
	gf, err := gguf.Read(m.Path)
	if err != nil {
		writeErr(w, 400, "not readable as GGUF: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"id":       id,
		"path":     m.Path,
		"version":  gf.Version,
		"nTensors": gf.NTensors,
		"summary":  gf.Summary,
		"metadata": gf.Metadata,
	})
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
	LastUsed  time.Time         `json:"lastUsed,omitempty"` // last gateway request (F060)
	Vars      map[string]string `json:"vars,omitempty"`
	LastError string            `json:"lastError,omitempty"`
	// AutoRestart: relaunch on crash (F087).
	AutoRestart bool `json:"autoRestart,omitempty"`
	// Inflight: gateway requests currently being served (F113).
	Inflight int64 `json:"inflight,omitempty"`
	// MaxParallel: gateway concurrency cap for this instance; 0 = none (F114).
	MaxParallel int `json:"maxParallel,omitempty"`
	// Note: free-text user annotation (F119).
	Note string `json:"note,omitempty"`
}

func (s *Server) listInstances(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	out := []instanceView{}
	for _, id := range st.SortedInstanceIDs() {
		i := st.Instances[id]
		v := instanceView{
			ID: i.ID, ModelID: i.ModelID, BackendID: i.BackendID,
			Transport: i.Transport, Port: i.Port, Status: i.Status,
			Attached: i.Attached, PID: i.PID, StartedAt: i.StartedAt,
			Vars: i.Vars, LastError: i.LastError, AutoRestart: i.AutoRestart,
			MaxParallel: i.MaxParallel, Note: st.InstanceNotes[i.ID],
		}
		if s.gw != nil {
			v.LastUsed = s.gw.ModelLastUsed(i.ModelID)
			v.Inflight = s.gw.InstInflight(i.ID)
		}
		out = append(out, v)
	}
	writeJSON(w, 200, out)
}

type startInstanceReq struct {
	Instance string `json:"instance"`
	Model    string `json:"model"`
	Backend  string `json:"backend"`
	Port     int    `json:"port"`
	Vars     any    `json:"vars"` // map of scalars; numbers/bools coerced to strings
	// AutoRestart relaunches the instance if it crashes (F087).
	AutoRestart bool `json:"autoRestart"`
}

// coerceVars accepts {"ctx": "64000"} and {"ctx": 64000} alike, mapping
// JSON scalars to strings. Returns an error if vars is not an object of
// scalars.
func coerceVars(v any) (map[string]string, error) {
	if v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("vars must be a JSON object")
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		switch t := val.(type) {
		case string:
			out[k] = t
		case float64:
			out[k] = strconv.FormatFloat(t, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(t)
		case nil:
			// skip nulls
		default:
			return nil, fmt.Errorf("vars[%q] must be a string, number, or bool", k)
		}
	}
	return out, nil
}

func (s *Server) startInstance(w http.ResponseWriter, r *http.Request) {
	var req startInstanceReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body: "+err.Error())
		return
	}
	vars, verr := coerceVars(req.Vars)
	if verr != nil {
		writeErr(w, 400, "invalid vars: "+verr.Error())
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		avail := []string{}
		if s.store != nil {
			if st, err := s.store.Load(); err == nil {
				for id := range st.Models {
					avail = append(avail, id)
				}
			}
		}
		sort.Strings(avail)
		if len(avail) == 0 {
			writeErr(w, 400, "model required — no models installed (install one on the Models page)")
		} else {
			writeErr(w, 400, "model required — installed: "+strings.Join(avail, ", "))
		}
		return
	}
	id := req.Instance
	if id == "" {
		id = req.Model
		// Default id taken? Derive model-2, model-3, ... so replicas of one
		// model can be started without naming each one.
		st, err := s.store.Load()
		if err == nil {
			running := func(candidate string) bool {
				if inst, ok := st.Instances[candidate]; ok && inst.Status == store.StatusRunning {
					return true
				}
				return false
			}
			for n := 2; running(id); n++ {
				id = fmt.Sprintf("%s-%d", req.Model, n)
			}
		}
	}
	// Start can be slow (model load). Run in a job so the UI can show progress,
	// but also return the instance once ready. We block here (bounded by the
	// health timeout) — the UI opens the request with a long timeout.
	s.startInstanceInternal(w, r, req, vars, id)
}

// startInstanceInternal performs the shared start flow for direct starts
// and preset starts (id already derived, vars already merged).
func (s *Server) startInstanceInternal(w http.ResponseWriter, r *http.Request, req startInstanceReq, vars map[string]string, id string) {
	inst, err := s.sv.Start(r.Context(), id, req.Model, req.Backend, req.Port, vars)
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	if req.AutoRestart {
		s.store.Update(func(st *store.State) error {
			if r := st.Instances[id]; r != nil {
				r.AutoRestart = true
			}
			return nil
		})
		inst.AutoRestart = true
	}
	writeJSON(w, 200, inst)
}

// setAutoRestart handles POST /api/v1/instances/{id}/autorestart — toggle
// crash auto-restart on a running or stopped instance (F087).
func (s *Server) setAutoRestart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	st, _ := s.store.Load()
	if st.Instances[id] == nil {
		writeErr(w, 404, "unknown instance: "+id)
		return
	}
	s.store.Update(func(st *store.State) error {
		if r := st.Instances[id]; r != nil {
			r.AutoRestart = req.Enabled
		}
		return nil
	})
	writeJSON(w, 200, map[string]any{"ok": true, "autoRestart": req.Enabled})
}

// setNote handles PATCH /api/v1/instances/{id}/note — free-text annotation
// shown on the Instances page (F119).
func (s *Server) setNote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	if len(req.Note) > 200 {
		writeErr(w, 400, "note too long (max 200 chars)")
		return
	}
	st, _ := s.store.Load()
	if st.Instances[id] == nil {
		writeErr(w, 404, "unknown instance: "+id)
		return
	}
	s.store.Update(func(st *store.State) error {
		if st.InstanceNotes == nil {
			st.InstanceNotes = map[string]string{}
		}
		if n := strings.TrimSpace(req.Note); n != "" {
			st.InstanceNotes[id] = n
		} else {
			delete(st.InstanceNotes, id)
		}
		return nil
	})
	writeJSON(w, 200, map[string]any{"ok": true})
}

// setMaxParallel handles PATCH /api/v1/instances/{id}/maxparallel — cap the
// number of simultaneous gateway requests this instance serves
// (0 = unlimited, F114).
func (s *Server) setMaxParallel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		MaxParallel int `json:"maxParallel"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	if req.MaxParallel < 0 {
		writeErr(w, 400, "maxParallel must be >= 0")
		return
	}
	st, _ := s.store.Load()
	if st.Instances[id] == nil {
		writeErr(w, 404, "unknown instance: "+id)
		return
	}
	s.store.Update(func(st *store.State) error {
		if r := st.Instances[id]; r != nil {
			r.MaxParallel = req.MaxParallel
		}
		return nil
	})
	writeJSON(w, 200, map[string]any{"ok": true, "maxParallel": req.MaxParallel})
}

// setInstanceVars handles PUT /api/v1/instances/{id}/vars — replace the
// stored start vars of an instance; a running instance needs a restart to
// apply them (F091).
func (s *Server) setInstanceVars(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Vars map[string]string `json:"vars"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	st, _ := s.store.Load()
	inst := st.Instances[id]
	if inst == nil {
		writeErr(w, 404, "unknown instance: "+id)
		return
	}
	s.store.Update(func(st *store.State) error {
		if r := st.Instances[id]; r != nil {
			r.Vars = req.Vars
		}
		return nil
	})
	writeJSON(w, 200, map[string]any{
		"ok":              true,
		"vars":            req.Vars,
		"restartRequired": inst.Status == store.StatusRunning,
	})
}

// setModelPrice handles PATCH /api/v1/models/{id}/price — set the USD
// per-1M-token price used for per-key cost estimates (F102).
func (s *Server) setModelPrice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		PriceInPerM  *float64 `json:"priceInPerM"`
		PriceOutPerM *float64 `json:"priceOutPerM"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	if (req.PriceInPerM != nil && *req.PriceInPerM < 0) || (req.PriceOutPerM != nil && *req.PriceOutPerM < 0) {
		writeErr(w, 400, "prices must be >= 0")
		return
	}
	st, _ := s.store.Load()
	if st.Models[id] == nil {
		writeErr(w, 404, "unknown model: "+id)
		return
	}
	s.store.Update(func(st *store.State) error {
		if m := st.Models[id]; m != nil {
			if req.PriceInPerM != nil {
				m.PriceInPerM = *req.PriceInPerM
			}
			if req.PriceOutPerM != nil {
				m.PriceOutPerM = *req.PriceOutPerM
			}
		}
		return nil
	})
	st, _ = s.store.Load()
	mm := st.Models[id]
	if s.gw != nil {
		s.gw.SetModelPricing(id, cost.ModelPricing{InputPerM: mm.PriceInPerM, OutputPerM: mm.PriceOutPerM})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "priceInPerM": mm.PriceInPerM, "priceOutPerM": mm.PriceOutPerM})
}

// setModelIdle handles PATCH /api/v1/models/{id}/idle — per-model idle-unload
// override: 0 = follow the global setting, -1 = never unload (pin), >0 =
// custom minutes (F136).
func (s *Server) setModelIdle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		IdleUnloadMin *int `json:"idleUnloadMin"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || req.IdleUnloadMin == nil {
		writeErr(w, 400, "expected {\"idleUnloadMin\": <minutes, -1 or 0>}")
		return
	}
	if *req.IdleUnloadMin < -1 {
		writeErr(w, 400, "idleUnloadMin must be -1 (never), 0 (global) or >0")
		return
	}
	st, _ := s.store.Load()
	if st.Models[id] == nil {
		writeErr(w, 404, "unknown model: "+id)
		return
	}
	s.store.Update(func(st *store.State) error {
		if m := st.Models[id]; m != nil {
			m.IdleUnloadMin = *req.IdleUnloadMin
		}
		return nil
	})
	writeJSON(w, 200, map[string]any{"ok": true, "idleUnloadMin": *req.IdleUnloadMin})
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

// stopAllInstances handles POST /api/v1/instances/stop-all — stop every
// managed instance (attached ones are left alone; they mirror external
// servers). F072.
func (s *Server) stopAllInstances(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Load()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	stopped := []string{}
	for _, id := range st.SortedInstanceIDs() {
		inst := st.Instances[id]
		if inst.Attached || inst.Status != store.StatusRunning {
			continue
		}
		if err := s.sv.Stop(id, false); err != nil {
			continue // best-effort: skip what won't stop
		}
		stopped = append(stopped, id)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "stopped": stopped})
}

// restartInstance handles POST /api/v1/instances/{id}/restart — stop and
// relaunch a running instance with the same id, port and vars (F066).
func (s *Server) restartInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st, err := s.store.Load()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	inst, ok := st.Instances[id]
	if !ok {
		writeErr(w, 404, "no instance "+id)
		return
	}
	if inst.Attached {
		writeErr(w, 409, "attached instances mirror an external server; restart that server instead")
		return
	}
	if inst.Status != store.StatusRunning {
		writeErr(w, 409, "instance is not running")
		return
	}
	if err := s.sv.Stop(id, false); err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	fresh, err := s.sv.Start(r.Context(), id, inst.ModelID, inst.BackendID, inst.Port, inst.Vars)
	if err != nil {
		writeErr(w, 500, "stopped, but restart failed: "+err.Error())
		return
	}
	writeJSON(w, 200, fresh)
}

// ---- auto-start specs ----

func (s *Server) listAutoStart(w http.ResponseWriter, r *http.Request) {
	specs, err := s.sv.AutoStartSpecs()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]store.AutoStartSpec, 0, len(specs))
	for _, v := range specs {
		out = append(out, *v)
	}
	writeJSON(w, 200, out)
}

func (s *Server) setAutoStart(w http.ResponseWriter, r *http.Request) {
	var spec store.AutoStartSpec
	var raw struct {
		Name      string `json:"name"`
		ModelID   string `json:"modelId"`
		BackendID string `json:"backendId"`
		Port      int    `json:"port"`
		Vars      any    `json:"vars"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&raw); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	vars, verr := coerceVars(raw.Vars)
	if verr != nil {
		writeErr(w, 400, "invalid vars: "+verr.Error())
		return
	}
	spec = store.AutoStartSpec{Name: raw.Name, ModelID: raw.ModelID, BackendID: raw.BackendID, Port: raw.Port, Vars: vars}
	if err := s.sv.SetAutoStart(spec); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) clearAutoStart(w http.ResponseWriter, r *http.Request) {
	model := r.PathValue("model")
	if err := s.sv.ClearAutoStart(model); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// metricsSnapshot serves gateway traffic metrics.
func (s *Server) metricsSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.gw == nil {
		writeErr(w, 503, "gateway not running")
		return
	}
	writeJSON(w, 200, s.gw.Metrics())
}

// dailyUsage handles GET /api/v1/usage/daily — the persisted UTC-date ->
// model usage rollup (F097). days=N limits the response to the newest N days.
func (s *Server) dailyUsage(w http.ResponseWriter, r *http.Request) {
	if s.gw == nil {
		writeErr(w, 503, "gateway not running")
		return
	}
	daily := s.gw.DailyUsage()
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && n > 0 {
		days := make([]string, 0, len(daily))
		for d := range daily {
			days = append(days, d)
		}
		sort.Strings(days)
		if len(days) > n {
			days = days[len(days)-n:]
		}
		out := make(map[string]map[string]*store.DailyModelUsage, len(days))
		for _, d := range days {
			out[d] = daily[d]
		}
		daily = out
	}
	writeJSON(w, 200, daily)
}

// dailyKeyUsage handles GET /api/v1/usage/keys — the persisted UTC-date ->
// key id usage rollup (F126). Same ?days=N windowing as /usage/daily;
// ?key=<id> limits to one key.
func (s *Server) dailyKeyUsage(w http.ResponseWriter, r *http.Request) {
	if s.gw == nil {
		writeErr(w, 503, "gateway not running")
		return
	}
	daily := s.gw.DailyKeyUsage()
	if key := r.URL.Query().Get("key"); key != "" {
		out := map[string]map[string]*store.DailyModelUsage{}
		for d, keys := range daily {
			if u, ok := keys[key]; ok {
				out[d] = map[string]*store.DailyModelUsage{key: u}
			}
		}
		daily = out
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && n > 0 {
		days := make([]string, 0, len(daily))
		for d := range daily {
			days = append(days, d)
		}
		sort.Strings(days)
		if len(days) > n {
			days = days[len(days)-n:]
		}
		out := make(map[string]map[string]*store.DailyModelUsage, len(days))
		for _, d := range days {
			out[d] = daily[d]
		}
		daily = out
	}
	writeJSON(w, 200, daily)
}

// first (F099). ?limit=N caps the response; ?kind= keeps only that event
// kind, e.g. instance.crashed (F116).
func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	if s.events == nil {
		writeJSON(w, 200, []events.Event{})
		return
	}
	limit := 50
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = n
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	writeJSON(w, 200, s.events.List(limit, kind))
}

// recentRequests handles GET /api/v1/requests — the ring of recent gateway
// requests (newest first, max 256 kept) (F061). Filters (F079): limit=N,
// model=<id>, status=<class "4" or exact "404">, minMs=<latency floor>,
// stream=1|0.
func (s *Server) recentRequests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if s.gw == nil {
		writeJSON(w, 200, []proxy.RequestLogEntry{})
		return
	}
	var statusExact, statusClass int
	if v := q.Get("status"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n >= 100 {
				statusExact = n
			} else if n >= 1 {
				statusClass = n
			}
		}
	}
	var minMs float64
	if v := q.Get("minMs"); v != "" {
		minMs, _ = strconv.ParseFloat(v, 64)
	}
	streamF := q.Get("stream") // "1" = streaming only, "0" = non-streaming
	model := q.Get("model")
	keyF := q.Get("key")       // match key id (exact) or key prefix (case-insensitive) (F120)
	instF := q.Get("instance") // serving replica id (F131)
	all := s.gw.RecentRequests(0)
	out := make([]proxy.RequestLogEntry, 0, len(all))
	for _, e := range all {
		if model != "" && e.Model != model {
			continue
		}
		if instF != "" && e.Instance != instF {
			continue
		}
		if keyF != "" && e.KeyID != keyF && !strings.HasPrefix(strings.ToLower(e.Key), strings.ToLower(keyF)) {
			continue
		}
		if statusExact != 0 && e.Status != statusExact {
			continue
		}
		if statusClass != 0 && e.Status/100 != statusClass {
			continue
		}
		if minMs > 0 && e.LatencyMs < minMs {
			continue
		}
		if streamF == "1" && !e.Stream {
			continue
		}
		if streamF == "0" && e.Stream {
			continue
		}
		out = append(out, e)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, 200, out)
}

// requestByID handles GET /api/v1/requests/{id} — one logged request looked
// up by its X-Request-Id (F104).
func (s *Server) requestByID(w http.ResponseWriter, r *http.Request) {
	if s.gw == nil {
		writeErr(w, 503, "gateway not running")
		return
	}
	e, ok := s.gw.RequestByID(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "request id not found (ring keeps the last 256 requests)")
		return
	}
	writeJSON(w, 200, e)
}

// instanceLogs returns the tail of an instance's stderr.log (and optionally
// stdout.log). Query params: stream=stdout|stderr (default stderr),
// bytes=max bytes to return from the end (default 64 KiB, max 1 MiB).
func (s *Server) instanceLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st, _ := s.store.Load()
	inst, ok := st.Instances[id]
	if !ok || inst.LogDir == "" {
		writeErr(w, 404, "unknown instance or no log dir: "+id)
		return
	}
	name := "stderr.log"
	if r.URL.Query().Get("stream") == "stdout" {
		name = "stdout.log"
	}
	maxBytes := 64 << 10
	if v := r.URL.Query().Get("bytes"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			if n > 1<<20 {
				n = 1 << 20
			}
			maxBytes = n
		}
	}
	tail, size, err := tailFile(filepath.Join(inst.LogDir, name), maxBytes)
	if err != nil {
		writeErr(w, 500, "read log: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"instance":  id,
		"stream":    strings.TrimSuffix(name, ".log"),
		"size":      size,
		"truncated": size > int64(maxBytes),
		"logs":      string(tail),
	})
}

// instanceLogsDownload handles GET /api/v1/instances/{id}/logs/download —
// serves the full log file as an attachment (F075).
func (s *Server) instanceLogsDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st, _ := s.store.Load()
	inst, ok := st.Instances[id]
	if !ok || inst.LogDir == "" {
		writeErr(w, 404, "unknown instance or no log dir: "+id)
		return
	}
	name := "stderr.log"
	if r.URL.Query().Get("stream") == "stdout" {
		name = "stdout.log"
	}
	path := filepath.Join(inst.LogDir, name)
	if _, err := os.Stat(path); err != nil {
		writeErr(w, 404, "no such log file: "+name)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, id, name))
	http.ServeFile(w, r, path)
}

// tailFile reads up to maxBytes from the end of path. Returns the bytes,
// the full file size, and any error. A missing file yields empty content.
func tailFile(path string, maxBytes int) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	size := fi.Size()
	start := int64(0)
	if size > int64(maxBytes) {
		start = size - int64(maxBytes)
		// Avoid splitting a UTF-8 sequence: skip continuation bytes.
		for start < size {
			buf := make([]byte, 1)
			if _, err := f.ReadAt(buf, start); err != nil {
				break
			}
			if buf[0]&0xC0 != 0x80 { // not a continuation byte
				break
			}
			start++
		}
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, size, err
	}
	tail, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)))
	if err != nil {
		return nil, size, err
	}
	return tail, size, nil
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
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Prefix        string     `json:"prefix"`
	CreatedAt     time.Time  `json:"createdAt"`
	TokenQuota    int64      `json:"tokenQuota,omitempty"`
	UsedTok       int64      `json:"usedTokens"`              // tokens spent since daemon start (F074)
	UsedCost      float64    `json:"usedCost"`                // estimated USD spent (F102)
	AllowedModels []string   `json:"allowedModels,omitempty"` // model allowlist, empty = all (F089)
	MaxConcurrent int        `json:"maxConcurrent,omitempty"` // in-flight cap, 0 = unlimited (F095)
	MaxRequests   int64      `json:"maxRequests,omitempty"`   // total request cap, 0 = unlimited (F105)
	UsedReqs      int64      `json:"usedRequests"`            // requests made since daemon start (F105)
	RateRPS       float64    `json:"rateRPS,omitempty"`       // sustained req/s, 0 = default (F130)
	RateBurst     int        `json:"rateBurst,omitempty"`     // burst size, 0 = default (F130)
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`     // nil = never (F135)
	// DailyCostBudgetUSD: estimated USD cap per UTC day, 0 = unlimited (F138).
	DailyCostBudgetUSD float64 `json:"dailyCostBudgetUSD,omitempty"`
	TodayCost          float64 `json:"todayCost"` // estimated USD spent today (F138)
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	out := []keyView{}
	for _, k := range st.APIKeys {
		v := keyView{ID: k.ID, Name: k.Name, Prefix: k.Prefix, CreatedAt: k.CreatedAt, TokenQuota: k.TokenQuota, AllowedModels: k.AllowedModels, MaxConcurrent: k.MaxConcurrent, MaxRequests: k.MaxRequests, RateRPS: k.RateRPS, RateBurst: k.RateBurst, ExpiresAt: k.ExpiresAt, DailyCostBudgetUSD: k.DailyCostBudgetUSD}
		if s.gw != nil {
			v.TodayCost = s.gw.KeyDailyCost(k.ID) // F138
		}
		if s.gw != nil {
			if ku, ok := s.gw.KeyUsage()[k.ID]; ok { // counters key on the key id (F109)
				v.UsedTok = ku.TotalTok
				v.UsedCost = ku.Cost
				v.UsedReqs = ku.Requests
			}
		}
		out = append(out, v)
	}
	writeJSON(w, 200, out)
}

type createKeyReq struct {
	Name          string   `json:"name"`
	TokenQuota    int64    `json:"tokenQuota"`    // 0 = unlimited (F074)
	AllowedModels []string `json:"allowedModels"` // empty = all models (F089)
	MaxConcurrent int      `json:"maxConcurrent"` // 0 = unlimited (F095)
	MaxRequests   int64    `json:"maxRequests"`   // 0 = unlimited (F105)
	RateRPS       float64  `json:"rateRPS"`       // sustained req/s, 0 = gateway default (F130)
	RateBurst     int      `json:"rateBurst"`     // burst size, 0 = gateway default (F130)
	ExpiresInDays int      `json:"expiresInDays"` // key dies after N days, 0 = never (F135)
	// DailyCostBudgetUSD caps estimated USD per UTC day for this key;
	// 0 = unlimited (F138).
	DailyCostBudgetUSD float64 `json:"dailyCostBudgetUSD"`
}

// updateKey handles PATCH /api/v1/keys/{id} — edit a key's model allowlist
// (F089).
func (s *Server) updateKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		AllowedModels      []string `json:"allowedModels"`
		MaxConcurrent      *int     `json:"maxConcurrent"`
		RateRPS            *float64 `json:"rateRPS"`            // 0 = gateway default (F130)
		RateBurst          *int     `json:"rateBurst"`          // 0 = gateway default (F130)
		ExpiresInDays      *int     `json:"expiresInDays"`      // 0 = never expires (F135)
		DailyCostBudgetUSD *float64 `json:"dailyCostBudgetUSD"` // 0 = unlimited (F138)
		Name               *string  `json:"name"`               // rename key (F140)
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	if req.MaxConcurrent != nil && *req.MaxConcurrent < 0 {
		writeErr(w, 400, "maxConcurrent must be >= 0")
		return
	}
	if req.RateRPS != nil && *req.RateRPS < 0 || req.RateBurst != nil && *req.RateBurst < 0 {
		writeErr(w, 400, "rateRPS and rateBurst must be >= 0")
		return
	}
	if req.ExpiresInDays != nil && *req.ExpiresInDays < 0 {
		writeErr(w, 400, "expiresInDays must be >= 0")
		return
	}
	if req.DailyCostBudgetUSD != nil && *req.DailyCostBudgetUSD < 0 {
		writeErr(w, 400, "dailyCostBudgetUSD must be >= 0")
		return
	}
	var expiresAt *time.Time
	if req.ExpiresInDays != nil && *req.ExpiresInDays > 0 {
		t := time.Now().AddDate(0, 0, *req.ExpiresInDays)
		expiresAt = &t
	}
	st, _ := s.store.Load()
	if st.APIKeys[id] == nil {
		writeErr(w, 404, "unknown key: "+id)
		return
	}
	list := normalizeModelList(req.AllowedModels)
	s.store.Update(func(st *store.State) error {
		if k := st.APIKeys[id]; k != nil {
			k.AllowedModels = list
			if req.MaxConcurrent != nil {
				k.MaxConcurrent = *req.MaxConcurrent
			}
			if req.RateRPS != nil {
				k.RateRPS = *req.RateRPS
			}
			if req.RateBurst != nil {
				k.RateBurst = *req.RateBurst
			}
			if req.ExpiresInDays != nil {
				k.ExpiresAt = expiresAt // nil when 0 (F135)
			}
			if req.DailyCostBudgetUSD != nil {
				k.DailyCostBudgetUSD = *req.DailyCostBudgetUSD
			}
			if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
				k.Name = strings.TrimSpace(*req.Name) // rename (F140)
			}
		}
		return nil
	})
	writeJSON(w, 200, map[string]any{"ok": true, "allowedModels": list})
}

// resetKeyUsage handles POST /api/v1/keys/{id}/reset-usage — zero the
// gateway's per-key usage counters (tokens, requests, cost) in memory and in
// persisted state (F107).
func (s *Server) resetKeyUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st, _ := s.store.Load()
	k := st.APIKeys[id]
	if k == nil {
		writeErr(w, 404, "unknown key: "+id)
		return
	}
	if s.gw != nil {
		s.gw.ResetKeyUsage(id) // counters key on the key id (F109)
	}
	s.store.Update(func(st *store.State) error {
		delete(st.KeyUsage, id)
		return nil
	})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var req createKeyReq
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	if req.TokenQuota < 0 {
		writeErr(w, 400, "tokenQuota must be >= 0")
		return
	}
	if req.RateRPS < 0 || req.RateBurst < 0 {
		writeErr(w, 400, "rateRPS and rateBurst must be >= 0")
		return
	}
	if req.ExpiresInDays < 0 {
		writeErr(w, 400, "expiresInDays must be >= 0")
		return
	}
	if req.DailyCostBudgetUSD < 0 {
		writeErr(w, 400, "dailyCostBudgetUSD must be >= 0")
		return
	}
	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		t := time.Now().AddDate(0, 0, req.ExpiresInDays)
		expiresAt = &t
	}
	plaintext := generateKey()
	sum := sha256.Sum256([]byte(plaintext))
	id := newID()
	prefix := plaintext
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	s.store.Update(func(st *store.State) error {
		st.APIKeys[id] = &store.APIKeyRecord{
			ID:                 id,
			Name:               req.Name,
			Hash:               hex.EncodeToString(sum[:]),
			Prefix:             prefix,
			CreatedAt:          time.Now(),
			RateRPS:            req.RateRPS,   // 0 = gateway default (F130)
			RateBurst:          req.RateBurst, // 0 = gateway default (F130)
			TokenQuota:         req.TokenQuota,
			AllowedModels:      normalizeModelList(req.AllowedModels),
			MaxConcurrent:      req.MaxConcurrent,
			MaxRequests:        req.MaxRequests,
			ExpiresAt:          expiresAt,              // nil = never (F135)
			DailyCostBudgetUSD: req.DailyCostBudgetUSD, // 0 = unlimited (F138)
		}
		return nil
	})
	// Return the plaintext exactly once.
	writeJSON(w, 201, map[string]any{
		"id":     id,
		"name":   req.Name,
		"key":    plaintext,
		"prefix": prefix,
		"note":   "store this key now; it is shown only once",
	})
}

// Delete an API key by ID.
func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st, _ := s.store.Load()
	if _, ok := st.APIKeys[id]; !ok {
		writeErr(w, 404, "unknown key")
		return
	}
	s.store.Update(func(st *store.State) error {
		delete(st.APIKeys, id)
		return nil
	})
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- settings ----

type settingsView struct {
	RequireAuth         bool     `json:"requireAuth"`
	IdleUnloadMin       int      `json:"idleUnloadMin"`
	NotifyWebhook       string   `json:"notifyWebhook"`
	NotifyWebhookSecret string   `json:"notifyWebhookSecret"`
	NotifyEvents        []string `json:"notifyEvents,omitempty"` // event-kind filter, empty = all (F143)
	AllowCORS           bool     `json:"allowCORS"`
	StartupTimeoutSec   int      `json:"startupTimeoutSec"`
	StartOnDemand       bool     `json:"startOnDemand"`
	MaxBodyMB           int      `json:"maxBodyMB"`
}

// testNotifyWebhook handles POST /api/v1/settings/notify-test — posts a
// test event to the configured notify webhook and reports the outcome
// (F094).
func (s *Server) testNotifyWebhook(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	url := strings.TrimSpace(st.Settings.NotifyWebhook)
	if url == "" {
		writeErr(w, 400, "no notify webhook configured")
		return
	}
	payload := map[string]any{
		"event":   "test",
		"message": "llmctl test notification",
		"time":    time.Now().UTC().Format(time.RFC3339),
	}
	if err := webhook.PostWithSecret(r.Context(), url, payload, st.Settings.NotifyWebhookSecret); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// webhookLog handles GET /api/v1/webhook/log — recent webhook delivery
// attempts, newest first (F144).
func (s *Server) webhookLog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, webhook.History(50))
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	writeJSON(w, 200, settingsView{
		RequireAuth:         st.Settings.RequireAuth,
		IdleUnloadMin:       st.Settings.IdleUnloadMin,
		NotifyWebhook:       st.Settings.NotifyWebhook,
		NotifyWebhookSecret: st.Settings.NotifyWebhookSecret,
		NotifyEvents:        st.Settings.NotifyEvents,
		AllowCORS:           st.Settings.AllowCORS,
		StartupTimeoutSec:   st.Settings.StartupTimeoutSec,
		StartOnDemand:       st.Settings.StartOnDemand,
		MaxBodyMB:           st.Settings.MaxBodyMB,
	})
}

type updateSettingsReq struct {
	RequireAuth         *bool     `json:"requireAuth"`
	IdleUnloadMin       *int      `json:"idleUnloadMin"`
	NotifyWebhook       *string   `json:"notifyWebhook"`
	NotifyWebhookSecret *string   `json:"notifyWebhookSecret"`
	NotifyEvents        *[]string `json:"notifyEvents"` // event-kind filter, [] = all (F143)
	AllowCORS           *bool     `json:"allowCORS"`
	StartupTimeoutSec   *int      `json:"startupTimeoutSec"`
	StartOnDemand       *bool     `json:"startOnDemand"`
	MaxBodyMB           *int      `json:"maxBodyMB"`
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.IdleUnloadMin != nil && *req.IdleUnloadMin < 0 {
		writeErr(w, 400, "idleUnloadMin must be >= 0")
		return
	}
	if req.StartupTimeoutSec != nil && (*req.StartupTimeoutSec < 0 || *req.StartupTimeoutSec > 86400) {
		writeErr(w, 400, "startupTimeoutSec must be 0–86400 (0 = default 240)")
		return
	}
	if req.MaxBodyMB != nil && (*req.MaxBodyMB < 0 || *req.MaxBodyMB > 4096) {
		writeErr(w, 400, "maxBodyMB must be 0–4096 (0 = default 64)")
		return
	}
	s.store.Update(func(st *store.State) error {
		if req.RequireAuth != nil {
			st.Settings.RequireAuth = *req.RequireAuth
		}
		if req.IdleUnloadMin != nil {
			st.Settings.IdleUnloadMin = *req.IdleUnloadMin
		}
		if req.NotifyWebhookSecret != nil {
			st.Settings.NotifyWebhookSecret = strings.TrimSpace(*req.NotifyWebhookSecret)
		}
		if req.NotifyWebhook != nil {
			st.Settings.NotifyWebhook = strings.TrimSpace(*req.NotifyWebhook)
		}
		if req.NotifyEvents != nil {
			clean := []string{}
			for _, e := range *req.NotifyEvents {
				if e = strings.TrimSpace(e); e != "" {
					clean = append(clean, e)
				}
			}
			st.Settings.NotifyEvents = clean // empty = forward all (F143)
		}
		if req.AllowCORS != nil {
			st.Settings.AllowCORS = *req.AllowCORS
		}
		if req.StartupTimeoutSec != nil {
			st.Settings.StartupTimeoutSec = *req.StartupTimeoutSec
		}
		if req.StartOnDemand != nil {
			st.Settings.StartOnDemand = *req.StartOnDemand
		}
		if req.MaxBodyMB != nil {
			st.Settings.MaxBodyMB = *req.MaxBodyMB
		}
		return nil
	})
	// Apply the change to the gateway immediately (runtime, no restart).
	if req.RequireAuth != nil && s.gw != nil {
		s.gw.SetRequireAuth(*req.RequireAuth)
	}
	if req.AllowCORS != nil && s.gw != nil {
		s.gw.SetAllowCORS(*req.AllowCORS)
	}
	if req.MaxBodyMB != nil && s.gw != nil {
		s.gw.SetMaxBodyMB(*req.MaxBodyMB)
	}
	// Return updated settings.
	st, _ := s.store.Load()
	writeJSON(w, 200, settingsView{
		RequireAuth:         st.Settings.RequireAuth,
		IdleUnloadMin:       st.Settings.IdleUnloadMin,
		NotifyWebhook:       st.Settings.NotifyWebhook,
		NotifyWebhookSecret: st.Settings.NotifyWebhookSecret,
		NotifyEvents:        st.Settings.NotifyEvents,
		AllowCORS:           st.Settings.AllowCORS,
		StartupTimeoutSec:   st.Settings.StartupTimeoutSec,
		StartOnDemand:       st.Settings.StartOnDemand,
		MaxBodyMB:           st.Settings.MaxBodyMB,
	})
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

// normalizeModelList trims, lowercases and de-duplicates a model allowlist,
// dropping empty entries (F089).
func normalizeModelList(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range in {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
