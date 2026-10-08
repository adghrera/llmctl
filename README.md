# llmctl

A lightweight, single-binary manager for running **multiple local LLMs** behind **multiple inference backends**, all exposed through one **OpenAI-compatible API endpoint** designed for high traffic and many concurrent clients.

Written in pure Go (stdlib only — no framework, no cgo): one static ~10 MB binary, goroutine-per-connection serving, zero-buffered SSE streaming.

```
                    ┌─────────────────────────────────────────┐
 clients ──HTTP──▶  │  llmctl gateway  :8080                  │
 OpenAI SDKs        │  /v1/chat/completions  /v1/completions  │
                    │  /v1/embeddings        /v1/models       │
                    │  auth · rate limit · concurrency cap    │
                    │  model→instance router (SSE passthrough)│
                    └───────┬──────────────┬──────────────────┘
                            │              │
                  ┌─────────▼───┐   ┌──────▼────────┐   ┌──────────────┐
                  │ llama.cpp   │   │ vLLM (attach) │   │ Ollama /     │
                  │ instance :A │   │ instance :B   │   │ LM Studio…   │
                  └─────────────┘   └───────────────┘   └──────────────┘
                    managed by the llmctl supervisor (launch / health / reap / stop)
```

## Why this framework choice

| Requirement | Choice | Why |
|---|---|---|
| High traffic | Go's `net/http` + `httputil.ReverseProxy` | mature, battle-tested HTTP/1.1+SSE path; same code as Kubernetes' ingress proxy |
| Many concurrent clients | goroutine-per-connection | ~2 KB stack each; tens of thousands of idle keep-alives are cheap |
| Lightweight | stdlib only, no framework | single static binary, no runtime deps, tiny RSS |
| Large streaming responses | `FlushInterval: -1` | every SSE chunk is flushed immediately — token-by-token latency is the backend's, not the proxy's |
| Safety under load | per-client token bucket + global in-flight cap | sheds load with `429` instead of melting |

Benchmarks in `go test`: 100 concurrent streaming clients through the gateway, plus a live check of 8 simultaneous inferences against a real 1.5B model (all 200s, ~1 s each).

## Quick start

```bash
go build -o bin/llmctl.exe ./cmd/llmctl   # build

llmctl up --api-key sk-my-secret &        # start gateway (:8080) + supervisor

llmctl backends install llama.cpp         # download llama-server (auto asset pick)
llmctl models install qwen2.5-1.5b-instruct-q4   # curated GGUF, resumable download
llmctl start qwen2.5-1.5b-instruct-q4 --var ctx=2048 --var ngl=0
```

Talk to it with any OpenAI client:

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-my-secret" \
  -d '{"model":"qwen2.5-1.5b-instruct-q4",
       "messages":[{"role":"user","content":"hello"}],"stream":true}'
```

```python
from openai import OpenAI
client = OpenAI(base_url="http://127.0.0.1:8080/v1", api_key="sk-my-secret")
stream = client.chat.completions.create(
    model="qwen2.5-1.5b-instruct-q4",
    messages=[{"role": "user", "content": "hello"}], stream=True)
```

## Commands

| Command | What it does |
|---|---|
| `llmctl up [--addr :8080] [--api-key K] [--rate 20] [--burst 60] [--max-inflight 512]` | run gateway + supervisor (foreground) |
| `llmctl down` | stop daemon + all owned instances |
| `llmctl status` / `ps` | daemon and instance state |
| `llmctl backends list\|install\|remove` | manage backend binaries |
| `llmctl models list\|available\|install\|remove` | manage model artifacts |
| `llmctl models install --hf <repo> [--file x.gguf]` | any HuggingFace GGUF repo |
| `llmctl start <model> [--instance n] [--port p] [--var k=v]` | launch a model instance |
| `llmctl stop <instance> [--force]` | graceful stop (SIGTERM grace, then kill) |
| `llmctl key generate` | random `sk-llm-…` gateway key |

## Backends

| Backend | Mode | Notes |
|---|---|---|
| `llama.cpp` | **install** (GitHub release) | GGUF on CPU/CUDA/Vulkan; `--asset vulkan\|cpu\|cuda-12.4` picks the build |
| `ollama` | attach | proxy an existing Ollama daemon (its `/v1` compat API) |
| `vllm` | attach | run `vllm serve` yourself; llmctl routes to it |
| `custom` | attach | any OpenAI-compatible server: LM Studio, TGI, Triton, remote |

Add more by editing `internal/manifest/manifest.json` — backends and models are data, not code.

## API endpoints

| Endpoint | Notes |
|---|---|
| `POST /v1/chat/completions` | streaming + non-streaming |
| `POST /v1/completions` | legacy completions passthrough |
| `POST /v1/embeddings` | passthrough |
| `GET /v1/models` | merged view of running instances |
| `GET /healthz` | liveness + in-flight + request counter |

Errors use the OpenAI error envelope (`{"error":{"message","type","code"}}`): `401` bad key, `404` unknown model, `429` rate/capacity, `503` no running instance, `502` upstream down.

## Operational model

- **State**: `~/.llmctl/state.json` — atomic writes + lockfile; CLI and daemon never race.
- **Instances**: each gets a free port, its own log dir (`~/.llmctl/logs/<id>/`), health-gated startup, crash reaping every 5 s.
- **Routing**: exact model-id match → prefix match → single-instance default (empty `model`).
- **Downloads**: resumable (HTTP Range), progress bar, sha256 on request.
- **Security**: gateway binds everything but the control API, which is `127.0.0.1`-only and unauthenticated by design — keep it loopback.

## Tests

```bash
go test ./...
```

Covers: SSE streaming passthrough, auth, rate limiting, model routing (found/404), model listing, and 100-way concurrent streaming.

## Limitations / roadmap

- Windows graceful stop escalates to `TerminateProcess` (no SIGTERM on Win32).
- `models remove` refuses to touch instances still using the file — stop first.
- Roadmap: API key-per-tenant quotas, load-balanced replicas of one model, P2P model cache, WebSocket passthrough.
