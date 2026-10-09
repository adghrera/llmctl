# llmctl — Feature Register

Every planned feature gets a stable ID. Update `status` when work is done
and verified. Statuses: NOT-STARTED, IN-PROGRESS, COMPLETED, IGNORE.

| ID | Priority | Order | Category | Title | Status | Description |
|----|----------|-------|----------|-------|--------|-------------|
| F001 | P0 | 1 | quality | Stdio protocol tests green | IN-PROGRESS | `go test ./...` fully passing, including the llmctl-stdio/1 session tests (hello, call, concurrent calls). |
| F002 | P0 | 2 | e2e | End-to-end verification of v0.2 | NOT-STARTED | Native run: daemon up on one port, install backend, download model, start instance, chat via /v1 with API key, stop, down. Both http (llama.cpp) and stdio (ref-stdio) paths. |
| F003 | P0 | 3 | ui | Chat interface | NOT-STARTED | UI page: pick a running model, send messages, see streamed tokens. Backed by the gateway /v1/chat/completions (SSE) using an API key. |
| F004 | P1 | 4 | docs | README v0.2 | NOT-STARTED | Document single-port daemon, management API, web UI, plugin backends, stdio transport, Docker usage. |
| F005 | P1 | 5 | docker | Docker support | NOT-STARTED | Multi-stage Dockerfile (build static binary, slim runtime), .dockerignore, volume for ~/.llmctl, port 8080. Verify `docker build` + `docker run` works. |
| F006 | P1 | 6 | platform | Cross-compile matrix | NOT-STARTED | `GOOS=linux GOARCH=amd64 go build` and `GOOS=darwin GOARCH=arm64 go build` succeed (stdlib-only, no cgo, so expected to work). |
| F007 | P2 | 7 | backends | Example plugin backend | NOT-STARTED | A documented example plugin JSON (docs/plugins/example.json) showing http + install kinds, plus a stdio example. |
| F008 | P2 | 8 | ui | UI polish | NOT-STARTED | Error toasts on failed actions, job history (recent jobs with results), confirm dialogs where destructive. |
| F009 | P2 | 9 | api | Per-key quotas | NOT-STARTED | Rate limits per API key (not just per client IP) with configurable rps/burst per key. |
| F010 | P3 | 10 | backends | Model replicas | NOT-STARTED | Start N instances of one model; gateway round-robins across them. |
| F011 | P3 | 11 | models | Curated model catalog | NOT-STARTED | Built-in list of popular GGUF models (name, size, quant) with one-click install in UI. |
| F012 | P4 | 12 | api | WebSocket passthrough | NOT-STARTED | Proxy /v1/ws (or similar) for backends exposing WebSocket APIs. |
| F013 | P4 | 13 | ui | Instance log viewer | NOT-STARTED | API endpoint + UI panel to tail an instance's stderr.log. |
| F014 | P5 | 14 | ops | Auto-start on boot | NOT-STARTED | Instances marked auto-start relaunch when the daemon starts. |
| F015 | P5 | 15 | api | Metrics endpoint | NOT-STARTED | /api/v1/metrics: request counts, in-flight, tokens served, latency percentiles. |

## Shipped (v0.2 architecture, this iteration)

| ID | Priority | Category | Title | Status | Description |
|----|----------|----------|-------|--------|-------------|
| F100 | P0 | architecture | Pluggable backend system | COMPLETED | `internal/backend`: Backend interface + Registry. Built-ins (llama.cpp, ollama, vllm, custom, ref-stdio) + JSON plugin backends from $LLMCTL_HOME/plugins/*.json (no recompile). |
| F101 | P0 | architecture | Stdio IPC transport | COMPLETED | `internal/stdio`: llmctl-stdio/1 JSON-lines protocol over stdin/stdout; concurrent request multiplexing by id; reference Python backend (echo mode or llama-server proxy). |
| F102 | P0 | architecture | Multi-instance supervisor | COMPLETED | `internal/supervisor`: http-process, stdio-process, and attach instance kinds; multiple backends/models running simultaneously; health-gated start, crash reaping, graceful stop. |
| F103 | P0 | api | Management API | COMPLETED | `internal/api` (/api/v1): backends (list/install/uninstall/add-plugin/remove), models (list/install/remove + HF search/files proxy), instances (list/start/stop), jobs (list + SSE events), keys (create/list/revoke, sha256-hashed), status, shutdown. |
| F104 | P0 | ui | Web UI | COMPLETED | `internal/ui`: embedded SPA (no CDN deps) — Overview, Backends (install + add plugin), Models (HF search, file picker, download), Instances (start with params, stop), API Keys. Live job progress via SSE. |
| F105 | P0 | architecture | Single-port daemon | COMPLETED | `internal/daemon`: gateway (/v1) + management API (/api/v1) + UI (/) on one port; hashed-key auth for the gateway; daemon.json for CLI discovery. |
| F106 | P0 | api | Async jobs with SSE | COMPLETED | `internal/jobs`: background install/download jobs, progress broadcast over /api/v1/jobs/events (SSE), shown live in UI. |
| F107 | P0 | cli | CLI v0.2 | COMPLETED | `cmd/llmctl`: thin client over the management API (up/down/status/ps/backends/models/start/stop/key). |
