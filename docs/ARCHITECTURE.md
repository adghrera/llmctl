# llmctl — Architecture

## Overview

llmctl is a single Go binary (stdlib only, no cgo) that manages local LLM
inference: it installs backends, downloads models, supervises running
instances, and fronts everything with one OpenAI-compatible gateway plus a
management API and web UI — all on **one port**.

```
                        ┌──────────────────────────────────────────────┐
 clients ──────────────▶│  llmctl daemon  (one port, default :8080)    │
 OpenAI SDKs / UI       │                                              │
                        │  /v1/*         OpenAI gateway                │
                        │    auth (hashed keys) · rate limit · cap     │
                        │    model→instance router                     │
                        │    http: reverse proxy (SSE, zero buffering) │
                        │    stdio: OpenAI↔llmctl-stdio/1 translator   │
                        │                                              │
                        │  /api/v1/*     management API                │
                        │    backends · models · instances · jobs      │
                        │    keys · hf search/files · status · shutdown│
                        │                                              │
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

## Packages

| Package | Responsibility |
|---------|----------------|
| `cmd/llmctl` | CLI. `up` runs the daemon; everything else is a thin client over /api/v1. |
| `internal/daemon` | Wires gateway + management API + UI onto one listener; daemon.json; signal handling. |
| `internal/proxy` | OpenAI gateway: auth (hashed keys, constant-time), per-client token bucket, in-flight cap, model→instance router, HTTP reverse proxy (FlushInterval -1), stdio translator. |
| `internal/api` | Management API handlers; job submission; HF search/files proxy; key management (sha256 hashes only). |
| `internal/backend` | Pluggable backend system: `Backend` interface, Registry (built-ins + JSON plugins), installers (GitHub release / URL / files). |
| `internal/stdio` | llmctl-stdio/1 protocol: Session (read loop, id-correlated pending map), Launch (spawn + pipes + hello wait). |
| `internal/supervisor` | Instance lifecycle: start (http/stdio/attach), health wait, stop, reap loop, in-memory stdio session map. |
| `internal/jobs` | Background job tracking + SSE event fan-out. |
| `internal/downloader` | Resumable (HTTP Range) downloads, sha256, HF resolve/catalog. |
| `internal/installer` | Backend binary install: GitHub release asset resolution (scans releases list, OS-token filtering), zip/tar.gz extract, direct URL. |
| `internal/store` | state.json: atomic writes + advisory file lock; backends, models, instances, api keys. |
| `internal/paths` | Disk layout under $LLMCTL_HOME (default ~/.llmctl). |
| `internal/ui` | Embedded SPA (embed.FS). |
| `internal/manifest` | Legacy v0.1 manifest (still used by nothing critical; slated for removal). |

## Key decisions

### One port, not two
v0.1 ran gateway (:8080) and control API (:8081) separately. v0.2 mounts
both plus the UI on a single mux. Simpler for users, Docker, and firewalling.
The gateway handler is exposed via `proxy.Server.Handler()` so the daemon
can compose it.

### Pluggable backends = interface + data
A `Backend` knows: how to build a launch `Spec` for a model, how to health
check, how to install. Built-ins are Go types; plugins are JSON files in
`$LLMCTL_HOME/plugins/*.json` implementing the same shape. No recompile to
add a backend. Plugin installers support `url`, `github-release`, and
`files` kinds.

### Stdio over HTTP, when supported
`llmctl-stdio/1`: one JSON object per line over the child's stdin/stdout.
- server→client hello: `{"protocol":"llmctl-stdio/1","name":...,"models":[...]}`
- client→server: `{"id":"<uuid>","method":"chat.completions","params":{...}}`
- server→client: `{"id":...,"type":"chunk"|"done"|"error","data":...}`
- keepalive: `ping` → `pong`

The Session multiplexes many concurrent requests over one pipe via a
pending map keyed by request id. The gateway translates OpenAI
chat.completions (streaming SSE and non-streaming) onto it. Backends that
only speak HTTP (llama.cpp) run as http-process instances; the reference
stdio backend (`ref-stdio`, Python) either proxies a launched llama-server
or runs in echo mode so the protocol works anywhere.

### Instance kinds
- **http process**: llmctl launches a binary on a free port, waits for
  health, reverse-proxies it.
- **stdio process**: llmctl launches a binary, holds the live Session in
  memory (supervisor.sessions map), translates requests.
- **attach**: user runs the server (ollama/vllm/custom/remote); llmctl
  verifies health and routes to the fixed URL.

### Auth
Gateway keys are stored as sha256 hashes in state.json; plaintext is shown
exactly once at creation. Verification is constant-time compare of digests.
Static keys (`--api-key`) still work for simple setups.

### State
Single `state.json` under $LLMCTL_HOME: atomic temp+rename writes, advisory
lock file. The router re-reads it lazily (mtime-checked) so CLI-driven
changes are picked up without IPC.

### Launch arg rendering (single place)
Backend `Spec.Args` use placeholders: `{model}` (absolute GGUF path),
`{dir}` (model dir), `{var:key}` (user vars like ctx/ngl/parallel), and
`{port}` (chosen free port). The supervisor's `renderLaunch` is the ONLY
place these are expanded — both the http-process and stdio-process start
paths call it. (Bug history: v0.2 first rendered only `{port}` in the
http path, so llama-server received `--ctx-size {var:ctx}` and exited.)

### Dialable daemon address
`daemon.json` must record an address the CLI can actually connect to. The
raw bind address for an unspecified host (`[::]:8080` / `0.0.0.0:8080`) is
not dialable on Windows, so `dialableAddr` maps it to loopback:
`::` → `[::1]:port`, `0.0.0.0` → `127.0.0.1:port`. Explicit hosts are kept.

### High-traffic design
- goroutine-per-connection; no framework overhead
- per-client token bucket (64 shards) + global in-flight cap → clean 429s
- SSE passthrough with `FlushInterval: -1` (token latency = backend's)
- stdio path: one pipe per instance, unbounded concurrent requests per pipe

## Disk layout

```
$LLMCTL_HOME (default ~/.llmctl)
  state.json          registry: backends, models, instances, api keys
  daemon.json         live daemon address + pid (removed on exit)
  llmctl.log          daemon log
  plugins/*.json      plugin backend descriptors
  backends/<id>/      installed backend binaries (versioned subdirs)
  models/<id>/        downloaded model artifacts
  logs/<instance>/    per-instance stdout/stderr
```

## Known limitations
- Windows graceful stop escalates to TerminateProcess (no SIGTERM).
- stdio backends currently translate /v1/chat/completions only.
- `internal/manifest` is legacy v0.1; remove once nothing references it.
- No per-key rate limits yet (per-client only) — F009.
