// Package proxy is the OpenAI-compatible gateway. It routes /v1/* requests
// to running backend instances by model name, streaming SSE through with
// zero buffering, and enforces API keys, per-key rate limits, and
// in-flight concurrency caps.
package proxy

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"llmctl/internal/cost"
	"llmctl/internal/store"
)

// Server is the gateway.
type Server struct {
	addr         string
	apiKeys      [][]byte // pre-encoded; empty list = auth disabled
	keyCheck     func(string) *store.APIKeyRecord // hashed-key verifier (overrides apiKeys)
	requireAuth  *atomic.Bool // if false, no key needed (auth disabled globally)
	allowCORS    *atomic.Bool                     // if true, CORS headers + OPTIONS preflight (F082)
	limiter      *rateLimiter
	router       *Router
	logger       *slog.Logger
	maxInflight  int64
	inflight     atomic.Int64
	requestCount atomic.Int64
	collector    *metricsCollector
	keyMu        sync.Mutex
	keyInflight  map[string]*atomic.Int64 // per key prefix, in-flight requests (F095)
	instInflight map[string]*atomic.Int64 // per instance id, in-flight requests (F113)
	instMu       sync.Mutex               // guards instInflight
	startHook    func(model string) bool  // daemon-provided cold-start attempt (F100)
	startMu      sync.Mutex               // serializes concurrent cold starts (F100)
	handler      http.Handler
	httpSrv      *http.Server
}

// Config for NewServer.
type Config struct {
	Addr        string
	APIKeys     []string // static keys (tests, simple setups)
	// KeyCheck, when non-nil, is the authoritative verifier (hashed keys).
	KeyCheck    func(token string) *store.APIKeyRecord
	RequireAuth bool // if false, auth is disabled regardless of keys
	AllowCORS   bool    // if true, gateway sends CORS headers + answers OPTIONS (F082)
	RateRPS     float64 // sustained requests/sec per client
	RateBurst   int
	MaxInflight int
	// RequestLogPath, when set, persists the recent-request ring to a JSONL
	// file so it survives daemon restarts (F086).
	RequestLogPath string
	Logger         *slog.Logger
}

func NewServer(cfg Config, router *Router) (*Server, error) {
	if cfg.MaxInflight <= 0 {
		cfg.MaxInflight = 512
	}
	if cfg.RateBurst <= 0 {
		cfg.RateBurst = 60
	}
	keys := make([][]byte, 0, len(cfg.APIKeys))
	for _, k := range cfg.APIKeys {
		if k != "" {
			keys = append(keys, []byte(k))
		}
	}
	// Default: enable auth when keys are configured. Setting RequireAuth:
	// false explicitly overrides this (useful for tests that want auth off).
	requireAuth := cfg.RequireAuth
	if !requireAuth && (len(keys) > 0 || cfg.KeyCheck != nil) {
		requireAuth = true
	}
	var getLimits func(string) (float64, int)
	if cfg.KeyCheck != nil {
		getLimits = func(token string) (float64, int) {
			if rec := cfg.KeyCheck(token); rec != nil {
				return rec.RateRPS, rec.RateBurst
			}
			return 0, 0
		}
	} else {
		getLimits = func(string) (float64, int) { return 0, 0 }
	}
	s := &Server{
		addr:        cfg.Addr,
		apiKeys:     keys,
		keyCheck:    cfg.KeyCheck,
		requireAuth: &atomic.Bool{},
		allowCORS:   &atomic.Bool{},
		limiter:     newRateLimiter(cfg.RateRPS, cfg.RateBurst, getLimits),
		router:      router,
		logger:      cfg.Logger,
		maxInflight: int64(cfg.MaxInflight),
		collector:   newMetricsCollector(),
		keyInflight:    map[string]*atomic.Int64{},
		instInflight:   map[string]*atomic.Int64{},
	}
	s.requireAuth.Store(requireAuth)
	s.allowCORS.Store(cfg.AllowCORS)
	if cfg.RequestLogPath != "" {
		s.collector.reqLog.useFile(cfg.RequestLogPath)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /v1/models", s.auth(s.listModels))
	mux.HandleFunc("POST /v1/chat/completions", s.auth(s.proxyChat))
	mux.HandleFunc("POST /v1/completions", s.auth(s.proxyCompletions))
	mux.HandleFunc("POST /v1/embeddings", s.auth(s.proxyEmbeddings))
	mux.HandleFunc("GET /v1/ws", s.auth(s.handleWS))
	ts := s.recoverer(s.metrics(s.cors(mux)))
	s.handler = ts
	s.httpSrv = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.handler,
		ReadHeaderTimeout: 30 * time.Second,
		// No global WriteTimeout: streaming responses are long-lived.
		IdleTimeout: 120 * time.Second,
	}
	return s, nil
}

// Handler returns the gateway's HTTP handler (auth + metrics + routes) so a
// daemon can mount it on a shared mux alongside the management API and UI.
func (s *Server) Handler() http.Handler { return s.handler }

// Metrics returns a snapshot of gateway traffic metrics (request counts,
// latency percentiles, tokens served, per-model breakdown).
func (s *Server) Metrics() map[string]any {
	snap := s.collector.Snapshot()
	snap["inflight"] = s.inflight.Load()
	return snap
}

// ModelLastUsed returns when the gateway last completed a request for the
// given model (zero time if never). Used by the idle-unload loop (F060).
func (s *Server) ModelLastUsed(model string) time.Time {
	return s.collector.lastUsedAt(model)
}

// InFlight returns the number of requests currently being served.
func (s *Server) InFlight() int64 { return s.inflight.Load() }

// RecentRequests returns up to n of the most recent gateway requests,
// newest first (n <= 0 = all kept, max 256).
func (s *Server) RecentRequests(n int) []RequestLogEntry {
	return s.collector.reqLog.recent(n)
}

// RequestByID looks up one logged request by its X-Request-Id (F104).
func (s *Server) RequestByID(id string) (RequestLogEntry, bool) {
	return s.collector.reqLog.byID(id)
}

// ListenAndServe blocks serving until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	go func() {
		<-ctx.Done()
		ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpSrv.Shutdown(ctx2)
	}()
	s.logger.Info("gateway listening", "addr", s.addr, "auth", len(s.apiKeys) > 0)
	err = s.httpSrv.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// ---- middleware ----

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic in handler", "panic", rec, "path", r.URL.Path)
				writeErr(w, http.StatusInternalServerError, "server_error", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requestCount.Add(1)
		// Per-request id (F092): reuse the client's X-Request-Id or mint one,
		// echo it back and attach it to the request log entry.
		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = newRequestID()
		}
		w.Header().Set("X-Request-Id", rid)
		if !s.tryAcquire() {
			writeErr(w, http.StatusTooManyRequests, "server_overloaded",
				fmt.Sprintf("gateway at capacity (%d in-flight requests)", s.maxInflight))
			return
		}
		defer s.inflight.Add(-1)
		// Record latency + status + tokens for /api/v1/metrics.
		holder := &modelRef{ID: rid}
		r = r.WithContext(withModelRef(r.Context(), holder))
		start := time.Now()
		rec := newRecordingResponseWriter(w)
		next.ServeHTTP(rec, r)
		var ttftMs float64
		if holder.Stream && !rec.firstWrite.IsZero() {
			ttftMs = float64(rec.firstWrite.Sub(start).Microseconds()) / 1000.0
		}
		s.collector.record(holder.Model, holder.Key, holder.KeyLabel, rec.status, float64(time.Since(start).Microseconds())/1000.0, ttftMs, holder.Stream, rec.buf.Bytes(), rid)
	})
}

// newRequestID mints a short random request id (F092).
func newRequestID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req-unknown"
	}
	return hex.EncodeToString(b[:])
}

// auth enforces Authorization: Bearer *** when keys are configured and
// requireAuth is true. If requireAuth is false, auth is skipped entirely.
// The setting is read dynamically on each request so changes to the
// settings store take effect without a daemon restart.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// If auth is disabled globally, skip key validation.
		if !s.requireAuth.Load() {
			// Still apply rate limiting keyed on IP.
			key := clientIP(r)
			if !s.limiter.allow(key) {
				writeErr(w, http.StatusTooManyRequests, "rate_limit_exceeded",
					fmt.Sprintf("gateway at capacity (%d in-flight requests)", s.maxInflight))
				return
			}
			next(w, r)
			return
		}

		got := bearerToken(r)
		var keyRecord *store.APIKeyRecord
		if s.keyCheck != nil {
			keyRecord = s.keyCheck(got)
		}
		valid := false
		if keyRecord != nil {
			valid = true
		} else if len(s.apiKeys) > 0 {
			for _, k := range s.apiKeys {
				if subtle.ConstantTimeCompare([]byte(got), k) == 1 {
					valid = true
					break
				}
			}
		}
		if s.keyCheck != nil || len(s.apiKeys) > 0 {
			if !valid {
				writeErr(w, http.StatusUnauthorized, "invalid_request_error",
					"missing or invalid API key (Authorization: Bearer ***)")
				return
			}
		}
		// Counter key: the unique key id when known (F109), else the 8-char
		// prefix. The old prefix-only keying collided because generated keys
		// share the "sk-llm-" prefix (only 1 hex char of variance).
		ckey := keyPrefix(got)
		if keyRecord != nil {
			ckey = keyRecord.ID
		}
		// Per-key token quota (F074): reject once the key has spent its budget.
		if keyRecord != nil && keyRecord.TokenQuota > 0 &&
			s.KeyTotalTokens(ckey) >= keyRecord.TokenQuota {
			writeErr(w, http.StatusTooManyRequests, "quota_exceeded",
				fmt.Sprintf("API key %q has used its token quota (%d tokens)", keyRecord.Name, keyRecord.TokenQuota))
			return
		}
		// Per-key request count quota (F105).
		if keyRecord != nil && keyRecord.MaxRequests > 0 &&
			s.KeyRequests(ckey) >= keyRecord.MaxRequests {
			writeErr(w, http.StatusTooManyRequests, "request_quota_exceeded",
				fmt.Sprintf("API key %q has used its request quota (%d requests)", keyRecord.Name, keyRecord.MaxRequests))
			return
		}
		// Per-key concurrency cap (F095): reject when the key already has
		// MaxConcurrent requests in flight.
		if keyRecord != nil && keyRecord.MaxConcurrent > 0 {
			release, ok := s.keyAcquire(ckey, keyRecord.MaxConcurrent)
			if !ok {
				writeErr(w, http.StatusTooManyRequests, "concurrency_exceeded",
					fmt.Sprintf("API key %q already has %d requests in flight", keyRecord.Name, keyRecord.MaxConcurrent))
				return
			}
			defer release()
		}
		// Per-client rate limit keyed on token (or IP when auth is off).
		key := got
		if key == "" {
			key = clientIP(r)
		}
		// Quota visibility (F108): tell the client what is left.
		if keyRecord != nil {
			if keyRecord.TokenQuota > 0 {
				used := s.KeyTotalTokens(ckey)
				left := keyRecord.TokenQuota - used
				if left < 0 {
					left = 0
				}
				w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(keyRecord.TokenQuota, 10))
				w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(left, 10))
			}
			if keyRecord.MaxRequests > 0 {
				used := s.KeyRequests(ckey)
				left := keyRecord.MaxRequests - used
				if left < 0 {
					left = 0
				}
				w.Header().Set("X-LLMCtrl-Requests-Limit", strconv.FormatInt(keyRecord.MaxRequests, 10))
				w.Header().Set("X-LLMCtrl-Requests-Remaining", strconv.FormatInt(left, 10))
			}
		}
		if !s.limiter.allow(key) {
			writeErr(w, http.StatusTooManyRequests, "rate_limit_exceeded",
				fmt.Sprintf("gateway at capacity (%d in-flight requests)", s.maxInflight))
			return
		}
		// Store counter key + readable prefix on modelRef for cost tracking.
		if ref := modelRefFrom(r.Context()); ref != nil {
			ref.Key = ckey
			ref.KeyLabel = keyPrefix(got)
			if keyRecord != nil {
				ref.AllowedModels = keyRecord.AllowedModels
			}
		}
		next(w, r)
	}
}

// ---- handlers ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"inflight": s.inflight.Load(),
		"requests": s.requestCount.Load(),
	})
}

// listModels merges every running instance's upstream /v1/models plus our
// own alias list so clients can discover routable model names.
func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	type modelObj struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	seen := map[string]bool{}
	data := []modelObj{}
	for _, inst := range s.router.RunningInstances() {
		// Clean alias first (what users should send as "model").
		if !seen[inst.ModelID] {
			seen[inst.ModelID] = true
			data = append(data, modelObj{inst.ModelID, "model", "llmctl"})
		}
		for _, m := range upstreamModels(inst) {
			if !seen[m] {
				seen[m] = true
				data = append(data, modelObj{m, "model", "llmctl"})
			}
		}
	}
	// Gateway aliases resolve on any request, so advertise them too (F065).
	aliases := s.router.Aliases()
	names := make([]string, 0, len(aliases))
	for a := range aliases {
		names = append(names, a)
	}
	sort.Strings(names)
	for _, a := range names {
		if !seen[a] {
			seen[a] = true
			data = append(data, modelObj{a, "model", "llmctl-alias"})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Server) proxyChat(w http.ResponseWriter, r *http.Request)      { s.proxyAny(w, r, "/v1/chat/completions") }
func (s *Server) proxyCompletions(w http.ResponseWriter, r *http.Request) { s.proxyAny(w, r, "/v1/completions") }
func (s *Server) proxyEmbeddings(w http.ResponseWriter, r *http.Request) { s.proxyAny(w, r, "/v1/embeddings") }

// proxyAny peeks the "model" field, resolves an upstream, and streams the
// request through. HTTP instances are reverse-proxied (SSE framing preserved
// exactly); stdio instances are translated over the llmctl-stdio/1 pipe.
func (s *Server) proxyAny(w http.ResponseWriter, r *http.Request, path string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20)) // 64 MiB request cap
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "cannot read body")
		return
	}
	var peek struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.Unmarshal(body, &peek) // model is optional; default instance may serve

	if ref := modelRefFrom(r.Context()); ref != nil {
		ref.Model = peek.Model
		ref.Stream = peek.Stream
		// Per-key model allowlist (F089): the requested name or its alias
		// target must be in the key's list.
		if len(ref.AllowedModels) > 0 {
			name := strings.ToLower(peek.Model)
			if target, ok := s.router.Aliases()[name]; ok {
				name = strings.ToLower(target)
			}
			allowed := false
			for _, m := range ref.AllowedModels {
				if strings.EqualFold(m, name) {
					allowed = true
					break
				}
			}
			if !allowed {
				writeErr(w, http.StatusForbidden, "permission_error",
					fmt.Sprintf("this API key may not call model %q", peek.Model))
				return
			}
		}
	}

	targets, err := s.router.ResolveList(peek.Model)
	if err != nil && s.startHook != nil && peek.Model != "" {
		// Start-on-demand (F100): try to cold-start a stopped instance for
		// this model, then resolve once more. Serialized so parallel
		// requests for a cold model start it only once.
		s.startMu.Lock()
		started := s.startHook(peek.Model)
		s.startMu.Unlock()
		if started {
			targets, err = s.router.ResolveList(peek.Model)
		}
	}
	if err != nil {
		status, code := routeError(err)
		writeErr(w, status, code, err.Error())
		return
	}

	// Least-loaded first so replicas spread work (F113); failover order is
	// still tried in full on connection-level failures.
	sort.SliceStable(targets, func(i, j int) bool {
		return s.instLoad(targets[i].InstanceID) < s.instLoad(targets[j].InstanceID)
	})

	if targets[0].Transport == "stdio" {
		s.instAdd(targets[0].InstanceID, 1)
		s.proxyStdio(w, r, targets[0], body, path, peek.Stream)
		s.instAdd(targets[0].InstanceID, -1)
		return
	}

	// HTTP: try replicas in order; on a connection-level failure before any
	// response bytes were written, fall over to the next replica (F070).
	var lastErr error
	for i, target := range targets {
		t := target
		s.instAdd(t.InstanceID, 1)
		upErr := s.proxyHTTP(w, r, body, t, path)
		s.instAdd(t.InstanceID, -1)
		if upErr == nil {
			return
		}
		lastErr = upErr
		if i == len(targets)-1 {
			break
		}
		s.logger.Warn("upstream failed, failing over", "instance", t.InstanceID,
			"next", targets[i+1].InstanceID, "err", upErr)
	}
	writeErr(w, http.StatusBadGateway, "api_error",
		fmt.Sprintf("no upstream could serve the request: %v", lastErr))
}

// startedWriter records whether any response bytes reached the client, so
// the failover loop only retries truly untouched requests.
type startedWriter struct {
	http.ResponseWriter
	started bool
}

func (w *startedWriter) WriteHeader(code int) { w.started = true; w.ResponseWriter.WriteHeader(code) }
func (w *startedWriter) Write(b []byte) (int, error) {
	w.started = true
	return w.ResponseWriter.Write(b)
}
func (w *startedWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// proxyHTTP reverse-proxies one attempt at target; it returns the upstream
// error if the connection failed before the response started, else nil.
func (s *Server) proxyHTTP(w http.ResponseWriter, r *http.Request, body []byte, target *Target, path string) error {
	var upErr error
	rw := &startedWriter{ResponseWriter: w}
	rp := &httputil.ReverseProxy{
		FlushInterval: -1, // flush every chunk: required for SSE token streaming
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target.BaseURL)
			// Incoming path is /v1/<endpoint>; map onto the instance's
			// API base (e.g. ollama also serves /v1, so this is identity
			// for OpenAI-native upstreams).
			pr.Out.URL.Path = joinPath(target.APIPath, strings.TrimPrefix(path, "/v1"))
			pr.Out.Host = target.BaseURL.Host
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) {
			upErr = e
		},
	}
	// Restore the peeked body on the outbound request.
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	r.ContentLength = int64(len(body))
	rp.ServeHTTP(rw, r)
	if upErr != nil {
		s.logger.Warn("upstream error", "instance", target.InstanceID, "err", upErr)
		if rw.started {
			return nil // response already in flight; nothing to retry
		}
		return upErr
	}
	return nil
}

// proxyStdio translates an OpenAI request into a llmctl-stdio/1 call and
// writes the response back — as SSE when stream=true, or a single JSON body
// otherwise.
func (s *Server) proxyStdio(w http.ResponseWriter, r *http.Request, target *Target, body []byte, path string, stream bool) {
	var params map[string]any
	if err := json.Unmarshal(body, &params); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON body")
		return
	}
	// Only chat.completions is translated over stdio; other endpoints fall
	// back to an error (the reference backend only implements chat).
	if path != "/v1/chat/completions" {
		writeErr(w, http.StatusNotImplemented, "not_implemented",
			"stdio backends currently support /v1/chat/completions only")
		return
	}
	params["stream"] = stream
	id := newReqID()
	messages, err := target.Session.Call(r.Context(), id, "chat.completions", params)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "api_error",
			fmt.Sprintf("stdio backend unavailable: %v", err))
		return
	}

	flusher, _ := w.(http.Flusher)
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		for m := range messages {
			switch m.Type {
			case "chunk":
				fmt.Fprintf(w, "data: %s\n\n", m.Data)
				if flusher != nil {
					flusher.Flush()
				}
			case "error":
				msg := "backend error"
				if m.Error != nil && m.Error.Message != "" {
					msg = m.Error.Message
				}
				writeErrInline(w, msg)
				return
			case "done":
				// Final chunk already streamed as a chunk; send sentinel.
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	// Non-streaming: collect the final object (last chunk or done.data).
	var final json.RawMessage
	for m := range messages {
		switch m.Type {
		case "chunk":
			final = m.Data
		case "error":
			msg := "backend error"
			if m.Error != nil && m.Error.Message != "" {
				msg = m.Error.Message
			}
			writeErr(w, http.StatusBadGateway, "api_error", msg)
			return
		case "done":
			if len(m.Data) > 0 {
				final = m.Data
			}
		}
	}
	if len(final) == 0 {
		writeErr(w, http.StatusBadGateway, "api_error", "stdio backend returned no data")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(final)
}

// writeErrInline emits an SSE error event (used mid-stream).
func writeErrInline(w http.ResponseWriter, msg string) {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": "api_error"}})
	fmt.Fprintf(w, "data: %s\n\n", b)
}

// newReqID makes a short unique request id for stdio correlation.
func newReqID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- helpers ----

func (s *Server) tryAcquire() bool {
	for {
		cur := s.inflight.Load()
		if cur >= s.maxInflight {
			return false
		}
		if s.inflight.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	// WebSocket clients (browsers) cannot set headers; allow the key as a
	// query parameter on upgrade requests.
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		if t := r.URL.Query().Get("access_token"); t != "" {
			return t
		}
	}
	return ""
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func joinPath(prefix, suffix string) string {
	return strings.TrimSuffix(prefix, "/") + suffix
}

func routeError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrNoRunning):
		return http.StatusServiceUnavailable, "model_not_loaded"
	default:
		return http.StatusNotFound, "model_not_found"
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// writeErr emits the OpenAI error envelope.
func writeErr(w http.ResponseWriter, code int, errType, msg string) {
	writeJSON(w, code, map[string]any{
		"error": map[string]any{"message": msg, "type": errType, "code": nil},
	})
}

// upstreamModels queries an instance's own /v1/models (1s budget).
func upstreamModels(inst *store.InstanceRecord) []string {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", inst.Port, modelsPath(inst)))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return nil
	}
	ids := make([]string, 0, len(out.Data))
	for _, d := range out.Data {
		ids = append(ids, d.ID)
	}
	return ids
}

func modelsPath(inst *store.InstanceRecord) string {
	base := inst.APIBasePath
	if base == "" {
		base = "/v1"
	}
	return strings.TrimSuffix(base, "/") + "/models"
}

// GenerateKey makes a random sk- prefixed API key.
func GenerateKey() string {
	b := make([]byte, 20)
	rand.Read(b)
	return "sk-llm-" + hex.EncodeToString(b)
}

// SetRequireAuth updates the gateway's auth requirement at runtime.
func (s *Server) SetRequireAuth(on bool) {
	s.requireAuth.Store(on)
}

// SetAllowCORS toggles CORS headers + OPTIONS preflight at runtime (F082).
func (s *Server) SetAllowCORS(on bool) {
	s.allowCORS.Store(on)
}

// cors answers cross-origin preflights and adds Access-Control-Allow-*
// headers when AllowCORS is enabled; otherwise it is a no-op (F082).
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowCORS.Load() {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SetKeyCheck updates the hashed-key verifier at runtime.
func (s *Server) SetKeyCheck(fn func(string) *store.APIKeyRecord) {
	s.keyCheck = fn
}

// SetStartHook installs a cold-start attempt used when no running instance
// serves a model: fn returns true if a start was attempted and may have
// succeeded (F100).
func (s *Server) SetStartHook(fn func(model string) bool) {
	s.startHook = fn
}

// InstInflight returns the number of gateway requests currently being served
// by one instance (F113).
func (s *Server) InstInflight(id string) int64 { return s.instLoad(id) }

// instLoad returns the current in-flight request count for one instance (F113).
func (s *Server) instLoad(id string) int64 {
	s.instMu.Lock()
	defer s.instMu.Unlock()
	if c := s.instInflight[id]; c != nil {
		return c.Load()
	}
	return 0
}

func (s *Server) instAdd(id string, delta int64) {
	s.instMu.Lock()
	defer s.instMu.Unlock()
	c := s.instInflight[id]
	if c == nil {
		c = &atomic.Int64{}
		s.instInflight[id] = c
	}
	c.Add(delta)
}

// keyAcquire increments the in-flight counter for a key prefix and returns a
// release func, or ok=false when the key is at its concurrency cap (F095).
func (s *Server) keyAcquire(prefix string, max int) (func(), bool) {
	s.keyMu.Lock()
	c := s.keyInflight[prefix]
	if c == nil {
		c = &atomic.Int64{}
		s.keyInflight[prefix] = c
	}
	s.keyMu.Unlock()
	if int(c.Add(1)) > max {
		c.Add(-1)
		return nil, false
	}
	var once sync.Once
	return func() { once.Do(func() { c.Add(-1) }) }, true
}

// keyPrefix returns the first 8 chars of a key for per-key cost tracking,
// or the raw value if shorter.
func keyPrefix(key string) string {
	if len(key) > 8 {
		return key[:8]
	}
	return key
}

// KeyUsage exposes per-key token totals (F074).
func (s *Server) KeyUsage() map[string]KeyUsage { return s.collector.keyUsage() }

// SetPricing replaces the per-model pricing table used for per-key cost
// estimates (F102).
func (s *Server) SetPricing(p map[string]cost.ModelPricing) {
	s.collector.setPricing(p)
}

// SetModelPricing updates one model's price immediately (F102).
func (s *Server) SetModelPricing(model string, p cost.ModelPricing) {
	s.collector.setModelPricing(model, p)
}

// DailyUsage returns the UTC-date -> model usage rollup (F097).
func (s *Server) DailyUsage() map[string]map[string]*store.DailyModelUsage {
	return s.collector.dailyUsage()
}

// SeedDaily restores the daily rollup from persisted state at startup (F097).
func (s *Server) SeedDaily(daily map[string]map[string]*store.DailyModelUsage) {
	s.collector.seedDaily(daily)
}

// KeyTotalTokens exposes one key counter's token total for quota checks (F074).
func (s *Server) KeyTotalTokens(prefix string) int64 { return s.collector.keyTotalTokens(prefix) }

// KeyRequests returns the number of requests billed to one key (F105).
func (s *Server) KeyRequests(prefix string) int64 {
	if u, ok := s.collector.keyUsage()[prefix]; ok {
		return u.Requests
	}
	return 0
}

// ResetKeyUsage zeroes the usage counters for one key (F107).
func (s *Server) ResetKeyUsage(prefix string) {
	s.collector.resetKeyUsage(prefix)
}

// SeedKeyUsage restores per-key counters from persisted state at startup (F080).
func (s *Server) SeedKeyUsage(usage map[string]KeyUsage) { s.collector.seedKeyUsage(usage) }

// ModelUsage exposes per-model token totals with estimated cost (F074).
func (s *Server) ModelUsage() map[string]KeyUsage { return s.collector.modelUsage() }
