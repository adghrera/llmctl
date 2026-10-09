# llmctl

A lightweight, single-binary manager for running **multiple local LLMs** on
**multiple inference backends**, all exposed through one **OpenAI-compatible
API** — with a **web UI** and a **management API** for everything. Designed
for high traffic and many concurrent clients.

Written in pure Go (stdlib only — no framework, no cgo): one static ~10 MB
binary, goroutine-per-connection serving, zero-buffered SSE streaming.

```
                        ┌──────────────────────────────────────────────┐
 clients ──────────────▶│  llmctl daemon  (one port, default :8080)    │
 OpenAI SDKs / UI       │                                              │
                        │  /v1/*         OpenAI gateway                │
                        │    auth (hashed keys) · rate limit · cap     │
                        │    model→instance router                     │
                        │    http: reverse proxy (SSE, zero buffering) │
                        │    stdio: OpenAI↔llmctl-stdio/1 translator   │
                        │  /api/v1/*     management API                │
                        │    backends · models · instances · jobs      │
                        │    keys · hf search/files · status · shutdown│
                        │  /*            embedded web UI (SPA)         │
                        └──────┬───────────────────────┬───────────────┘
                               │                       │
                 ┌─────────────▼───────┐     ┌─────────▼──────────┐
                 │ http-process        │     │ stdio-process      │
                 │ llama.cpp :port     │     │ ref-stdio (python) │
                 │ (reverse proxied)   │     │ JSON-lines on      │
                 └─────────────────────┘     │ stdin/stdout, no   │
                 ┌─────────────────────┐     │ port               │
                 │ attach              │     └────────────────────┘
                 │ ollama / vllm /     │
                 │ custom / remote     │
                 └─────────────────────┘
```

## Why this framework choice

| Requirement | Choice | Why |
|---|---|---|
| High traffic | Go's `net/http` + `httputil.ReverseProxy` | mature HTTP/1.1+SSE path; same code as Kubernetes' ingress proxy |
| Many concurrent clients | goroutine-per-connection | ~2 KB stack each; tens of thousands of idle keep-alives are cheap |
| Lightweight | stdlib only, no framework | single static binary, no runtime deps, tiny RSS |
| Large streaming responses | `FlushInterval: -1` | every SSE chunk is flushed immediately — token latency is the backend's |
| Safety under load | per-client token bucket + global in-flight cap | sheds load with `429` instead of melting |
| IPC without ports | `llmctl-stdio/1` JSON-lines | backends that support it run over stdin/stdout — no port, no HTTP overhead, many concurrent requests per pipe |

## Quick start

```bash
go build -o bin/llmctl.exe ./cmd/llmctl   # build

llmctl up                                  # daemon on :8080 (UI + /v1 + /api/v1)
# web UI:  http://127.0.0.1:8080/

llmctl backends install llama.cpp          # download llama-server (auto asset pick)
llmctl models install Qwen/Qwen2.5-1.5B-Instruct-GGUF --file qwen2.5-1.5b-instruct-q4_k_m.gguf
llmctl key generate my-app                 # -> sk-llm-…  (shown once)
llmctl start qwen2.5-1.5b-instruct-q4 --var ctx=2048 --var ngl=0
```

Talk to it with any OpenAI client:

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer ***" \
  -d '{"model":"qwen2.5-1.5b-instruct-q4",
       "messages":[{"role":"user","content":"hello"}],"stream":true}'
```

```python
from openai import OpenAI
client = OpenAI(base_url="http://127.0.0.1:8080/v1", api_key="sk-llm-…")
stream = client.chat.completions.create(
    model="qwen2.5-1.5b-instruct-q4",
    messages=[{"role": "user", "content": "hello"}], stream=True)
```

## One port, three surfaces

`llmctl up` serves everything on a single port (default `:8080`):

| Path | What |
|---|---|
| `/` | embedded web UI (Overview, Backends, Models, Instances, **Chat**, API Keys) |
| `/v1/*` | OpenAI-compatible gateway (auth + rate limit + routing) |
| `/api/v1/*` | management API (backends, models, instances, jobs, keys, HF proxy) |

The gateway requires a valid API key (`Authorization: Bearer sk-llm-…`).
Create keys in the UI or with `llmctl key generate`. Keys are stored as
sha256 hashes; the plaintext is shown exactly once.

## Commands

| Command | What it does |
|---|---|
| `llmctl up [--addr :8080] [--rate 20] [--burst 60] [--max-inflight 512] [--api-key K]` | run the daemon (foreground) |
| `llmctl down` | stop daemon + all owned instances |
| `llmctl status` / `ps` | daemon and instance state |
| `llmctl backends list\|install\|uninstall\|add\|remove` | manage backend binaries + plugins |
| `llmctl models list\|install\|remove\|hf-search\|hf-files` | manage model artifacts |
| `llmctl start <model> [--backend id] [--instance n] [--var k=v]` | launch a model instance |
| `llmctl stop <instance> [--force]` | graceful stop (signal grace, then kill) |
| `llmctl key generate\|list\|revoke` | gateway API keys |
| `llmctl version` | version |

## Backends (pluggable)

Built-in:

| Backend | Mode | Transport | Notes |
|---|---|---|---|
| `llama.cpp` | **install** (GitHub release) | http | GGUF on CPU/CUDA/Vulkan; `--asset vulkan\|cpu\|cuda-12.4` picks the build |
| `ref-stdio` | **install** (embedded) | stdio | reference `llmctl-stdio/1` backend (Python); proxies a `llama-server` if on PATH, else echo mode |
| `ollama` | attach | http | proxy an existing Ollama daemon (its `/v1` compat API) |
| `vllm` | attach | http | run `vllm serve` yourself; llmctl routes to it |
| `custom` | attach | http | any OpenAI-compatible server: LM Studio, TGI, Triton, remote |

**Plugins** — add a backend without recompiling. Drop a JSON descriptor in
`~/.llmctl/plugins/*.json` (or use the UI / `llmctl backends add`):

```json
{
  "id": "my-backend", "name": "My Backend",
  "transport": "http", "kind": "process",
  "binary": "my-server",
  "args": ["--port", "{port}", "--model", "{model}"],
  "healthPath": "/health",
  "install": { "kind": "github-release", "repo": "owner/repo", "binaryName": "my-server" }
}
```

Placeholders: `{model}` (GGUF path), `{dir}` (model dir), `{var:key}` (user
vars), `{port}` (chosen free port). Install kinds: `url`, `github-release`,
`files`.

## Management API

All UI features are API-backed (agents/CI use the same surface). Base:
`http://127.0.0.1:8080/api/v1`.

| Endpoint | Purpose |
|---|---|
| `GET /status` | daemon + counts |
| `GET /backends` · `POST /backends/{id}/install` · `POST /backends/{id}/uninstall` | backends |
| `POST /backends` · `DELETE /backends/{id}` | add / remove plugin |
| `GET /models` · `POST /models/install` · `DELETE /models/{id}` | models |
| `GET /hf/search?q=` · `GET /hf/files?repo=` | HuggingFace search + file list (proxied) |
| `GET /instances` · `POST /instances` · `DELETE /instances/{id}` | start / stop instances |
| `GET /jobs` · `GET /jobs/events` (SSE) | background jobs + live progress |
| `GET /keys` · `POST /keys` · `DELETE /keys/{id}` | API keys |
| `POST /shutdown` | graceful daemon stop |

## Operational model

- **State**: `~/.llmctl/state.json` — atomic writes + lockfile; CLI and daemon never race.
- **Instances**: each gets a free port (http) or a stdio pipe, its own log dir (`~/.llmctl/logs/<id>/`), health-gated startup, crash reaping every 5 s.
- **Routing**: exact model-id match → prefix match → single-instance default (empty `model`).
- **Downloads**: resumable (HTTP Range), progress shown live in the UI (SSE), sha256 on request.
- **Security**: gateway keys are sha256-hashed; the management API and UI are served on the same port — bind to loopback (`127.0.0.1`) unless you add your own auth in front.

## Tests

```bash
go test ./...
```

Covers: SSE streaming passthrough, auth, rate limiting, model routing, the
`llmctl-stdio/1` session protocol (hello, call, 50-way concurrent calls), and
launch-arg rendering.

## Limitations / roadmap

- Windows graceful stop escalates to `TerminateProcess` (no SIGTERM on Win32).
- stdio backends currently translate `/v1/chat/completions` only.
- `models remove` refuses to touch instances still using the file — stop first.
- Roadmap (see `docs/PLAN.md`): per-key quotas, load-balanced replicas,
  curated model catalog, WebSocket passthrough, instance log viewer,
  auto-start on boot, metrics.

## Docker

```bash
docker build -t llmctl .
docker run -d --name llmctl -p 8080:8080 \
  -v llmctl-data:/root/.llmctl llmctl
# UI + /v1 + /api/v1 on http://127.0.0.1:8080
```

The runtime image is `python:3.12-slim` (python3 is needed by the `ref-stdio`
backend); the Go binary is static. Installed backends, models, and state live
in the `/root/.llmctl` volume. The daemon binds all interfaces inside the
container, so `-p` port mapping works.

## Docs

- `docs/ARCHITECTURE.md` — how the system fits together
- `docs/PRODUCT.md` — what's shipped + design principles
- `docs/PLAN.md` — feature roadmap
- `FEATURES.md` — feature register with statuses
- `AGENTS.md` — working rules for AI agents
