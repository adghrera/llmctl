# llmctl — Product

## What it is

A lightweight, single-binary manager for running **multiple local LLMs** on
**multiple backends**, all served through one **OpenAI-compatible API** —
with a web UI and a management API for everything.

## Shipped (v0.2)

### One command, one port
`llmctl up` starts everything on `:8080`:
- `http://127.0.0.1:8080/` — web UI
- `http://127.0.0.1:8080/v1/...` — OpenAI-compatible gateway
- `http://127.0.0.1:8080/api/v1/...` — management API

### Backends (pluggable)
Built-in:
- **llama.cpp** — installed from GitHub releases (auto asset pick per
  OS; `--asset` hint to choose CPU/Vulkan/CUDA builds). GGUF on CPU/GPU.
- **ref-stdio** — reference stdio backend (Python). Materialized from the
  binary; proxies a llama-server if one is on PATH, else echo mode.
- **ollama / vllm / custom** — attach to servers you run yourself
  (OpenAI-compatible HTTP).

Plugins: drop a JSON descriptor in `~/.llmctl/plugins/` (or use the UI /
`llmctl backends add`) — http or stdio transport, installed from a URL,
GitHub release, or file list. No recompile.

### Models
- Download from any HuggingFace repo (search + file picker in the UI, or
  `llmctl models install <repo> --file <f.gguf>`).
- Direct URLs work too.
- Resumable downloads (HTTP Range), progress shown live in the UI (SSE).
- Multiple models can be installed and run at the same time.

### Instances
- Start/stop from UI or CLI; multiple instances (different models,
  different backends, or replicas) run concurrently.
- Per-instance parameters: ctx, ngl (GPU layers), parallel slots, threads,
  url (attach backends).
- Health-gated startup; crashed instances are reaped and marked stopped.
- Logs per instance under `~/.llmctl/logs/<instance>/`.

### API keys
- Generate in UI or CLI; shown once, stored as sha256 hash.
- Gateway auth (require API key on /v1/*) is OFF by default on a fresh install — chat works without a key.
- Turn it ON in Settings → Require API Key (or start with `--api-key K`) to reject requests without a valid key (401).
- Revoke keys anytime on the API Keys page.

### Management API (all UI features are API-backed)
| Endpoint | Purpose |
|---|---|
| `GET /api/v1/status` | daemon + counts |
| `GET /api/v1/backends` | list backends (installed state, plugin flag) |
| `POST /api/v1/backends/{id}/install` | start install job |
| `POST /api/v1/backends/{id}/uninstall` | remove backend files |
| `POST /api/v1/backends` | add plugin (JSON body) |
| `DELETE /api/v1/backends/{id}` | remove plugin |
| `GET /api/v1/models` | list installed models |
| `POST /api/v1/models/install` | start download job (repo/file/rev/backend) |
| `DELETE /api/v1/models/{id}` | delete model file |
| `GET /api/v1/hf/search?q=` | HuggingFace model search (proxied) |
| `GET /api/v1/hf/files?repo=` | list GGUF files in a repo |
| `GET /api/v1/instances` | list instances |
| `POST /api/v1/instances` | start instance (model, backend, vars) |
| `DELETE /api/v1/instances/{id}` | stop instance |
| `GET /api/v1/jobs` | job list |
| `GET /api/v1/jobs/events` | SSE job progress stream |
| `GET /api/v1/keys` / `POST /api/v1/keys` / `DELETE /api/v1/keys/{id}` | API keys |
| `POST /api/v1/shutdown` | graceful daemon stop |

### Gateway
- `POST /v1/chat/completions` (streaming + non-streaming)
- `POST /v1/completions`, `POST /v1/embeddings` (http instances)
- `GET /v1/models` (merged view of running instances)
- `GET /healthz`
- OpenAI error envelope; 401/404/429/502/503 semantics.
- Per-client rate limit + global in-flight cap.

### Web UI
Embedded SPA (no CDN deps — works offline). Tabs:
- **Overview** — status counts, running instances, active jobs.
- **Backends** — install/uninstall built-ins, add plugin backends (JSON).
- **Models** — HuggingFace search + file picker, download with live progress.
- **Instances** — start (with ctx/ngl/parallel/threads/url vars) / stop.
- **Chat** — pick a running model, enter/generate an API key, send messages,
  stream tokens live (SSE), multi-turn history. Each message carries estimated
  token usage (~4 chars/token); the conversation shows a context-window bar
  (used vs. model limit). When context reaches 98% of the model's limit,
  older messages are auto-trimmed (system prompt and recent turns kept).
- **Usage** — per-model token bar chart (from /api/v1/metrics) and
  per-conversation token table (from /api/v1/chats). Shows prompt/completion
  totals, request counts, and per-conversation context percentage.
- **Tools** — install agent tools from a URL (.zip) or a local folder
  (`~/.llmctl/tools/<id>/`, described by `llmctl-tool.json` with a Python
  `def run(args)` entry). Tools run inside the llm-sandbox framework
  (container, no network, memory & PID caps) by default; a per-tool toggle
  opts out to host execution. List, enable/disable, test-run with args, and
  uninstall. Enabled tools join the agent's tool set in Chat.
- **Skills** — create reusable prompt templates invoked in chat via
  `/s.<short_name> <text>`. Each skill has a short name, display name,
  description, prompt template (with {{input}} and {{conversation}} placeholders),
  tags, and an enabled flag. Stored as directories at
  `~/.llmctl/skills/<id>/`. The Skills page supports add/edit/remove/enable/disable.
  When invoked, the rendered skill prompt is inserted as a user message right
  before the user's input, after prior conversation history. Works in both agent
  and plain chat modes.
- **Conversation search** — full-text search across all saved conversations
  (titles and message content) via `GET /api/v1/chats/search?q=`. Returns
  matching conversations with message snippets. Search bar in the Chat UI's
  Conversations card; click a result to open it.
- **Conversation fork** — fork a saved conversation at any message index via
  `POST /api/v1/chats/{id}/fork` with body `{"forkAt": N, "title": "..."}`.
  Creates a new conversation copying the first N messages. Frontend: Fork
  button on each conversation opens a dialog to pick fork point and title.
- **Memory** — persistent long-term memory for the agent. Entries have text
  content, tags, and source, stored at `~/.llmctl/memory/<id>/`. REST API:
  GET/POST/GET/PUT/DELETE /api/v1/memory, GET /api/v1/memory/search?q=. During
  agent chat, relevant memories are searched using the last user message and
  injected as system context. Dedicated Memory page to add/delete entries.
- **API Keys** — generate / list / revoke.

Live job progress streams over SSE. Chat state (key, draft, conversation)
survives the periodic UI re-render.

## CLI

```
llmctl up [--addr :8080] [--rate 20] [--burst 60] [--max-inflight 512] [--api-key K]
llmctl down
llmctl status
llmctl ps
llmctl backends list|install|uninstall|add|remove
llmctl models list|install|remove|hf-search|hf-files
llmctl start <model> [--backend id] [--instance name] [--var k=v ...]
llmctl stop <instance> [--force]
llmctl key generate|list|revoke
llmctl chat [--model ID] [--key KEY]
llmctl version
```

## Design principles
- Simple over clever: stdlib Go, one binary, one port, one state file.
- Everything the UI does is an API call (agents/CI can use the same surface).
- Pluggable by data (JSON), not by code, for common cases.
- Prefer stdio IPC for backends that support it; HTTP otherwise.

## Not yet (see docs/PLAN.md)
Docker image, per-key quotas, model replicas, curated catalog,
WebSocket passthrough, log viewer, auto-start, metrics.
