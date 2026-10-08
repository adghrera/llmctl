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
	"strings"
	"sync/atomic"
	"time"

	"llmctl/internal/store"
)

// Server is the gateway.
type Server struct {
	addr         string
	apiKeys      [][]byte // pre-encoded; empty list = auth disabled
	limiter      *rateLimiter
	router       *Router
	logger       *slog.Logger
	maxInflight  int64
	inflight     atomic.Int64
	requestCount atomic.Int64
	daemonPort   int // for the self-routing fallback (0 = disabled)
	httpSrv      *http.Server
}

// Config for NewServer.
type Config struct {
	Addr         string
	APIKeys      []string
	RateRPS      float64 // sustained requests/sec per client
	RateBurst    int
	MaxInflight  int
	Logger       *slog.Logger
	DaemonPort   int
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
	s := &Server{
		addr:        cfg.Addr,
		apiKeys:     keys,
		limiter:     newRateLimiter(cfg.RateRPS, cfg.RateBurst),
		router:      router,
		logger:      cfg.Logger,
		maxInflight: int64(cfg.MaxInflight),
		daemonPort:  cfg.DaemonPort,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /v1/models", s.auth(s.listModels))
	mux.HandleFunc("POST /v1/chat/completions", s.auth(s.proxyChat))
	mux.HandleFunc("POST /v1/completions", s.auth(s.proxyCompletions))
	mux.HandleFunc("POST /v1/embeddings", s.auth(s.proxyEmbeddings))
	s.httpSrv = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.recoverer(s.metrics(mux)),
		ReadHeaderTimeout: 30 * time.Second,
		// No global WriteTimeout: streaming responses are long-lived.
		IdleTimeout: 120 * time.Second,
	}
	return s, nil
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
		if !s.tryAcquire() {
			writeErr(w, http.StatusTooManyRequests, "server_overloaded",
				fmt.Sprintf("gateway at capacity (%d in-flight requests)", s.maxInflight))
			return
		}
		defer s.inflight.Add(-1)
		next.ServeHTTP(w, r)
	})
}

// auth enforces Authorization: Bearer <key> when keys are configured.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(s.apiKeys) > 0 {
			got := bearerToken(r)
			ok := false
			for _, k := range s.apiKeys {
				if subtle.ConstantTimeCompare([]byte(got), k) == 1 {
					ok = true
					break
				}
			}
			if !ok {
				writeErr(w, http.StatusUnauthorized, "invalid_request_error",
					"missing or invalid API key (Authorization: Bearer <key>)")
				return
			}
		}
		// Per-client rate limit keyed on token (or IP when auth is off).
		key := bearerToken(r)
		if key == "" {
			key = clientIP(r)
		}
		if !s.limiter.allow(key) {
			writeErr(w, http.StatusTooManyRequests, "rate_limit_exceeded",
				"rate limit exceeded; retry shortly")
			return
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
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Server) proxyChat(w http.ResponseWriter, r *http.Request)      { s.proxyAny(w, r, "/v1/chat/completions") }
func (s *Server) proxyCompletions(w http.ResponseWriter, r *http.Request) { s.proxyAny(w, r, "/v1/completions") }
func (s *Server) proxyEmbeddings(w http.ResponseWriter, r *http.Request) { s.proxyAny(w, r, "/v1/embeddings") }

// proxyAny peeks the "model" field, resolves an upstream, and streams the
// request through, preserving SSE framing exactly as the backend emits it.
func (s *Server) proxyAny(w http.ResponseWriter, r *http.Request, path string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20)) // 64 MiB request cap
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request_error", "cannot read body")
		return
	}
	var peek struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &peek) // model is optional; default instance may serve

	target, err := s.router.Resolve(peek.Model)
	if err != nil {
		status, code := routeError(err)
		writeErr(w, status, code, err.Error())
		return
	}

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
			s.logger.Warn("upstream error", "instance", target.InstanceID, "err", e)
			writeErr(w, http.StatusBadGateway, "api_error",
				fmt.Sprintf("upstream instance %q unavailable: %v", target.InstanceID, e))
		},
	}
	// Restore the peeked body on the outbound request.
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	r.ContentLength = int64(len(body))
	rp.ServeHTTP(w, r)
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
