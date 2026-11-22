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
- **gufo** — Gufo OpenAI-compatible engine for AMD Strix Halo
  (`gufo serve llm --model …`). Launched as a process; readiness via `/ready`.
  Installs the Windows zip from GitHub releases; on Linux it uses a `gufo`
  already on PATH (gufo ships a `.deb`/nix/container there). Vars: `ctx`,
  `servedName`.
- **halogen** — attach to a Halogen flash server (OpenAI-compatible, AMD Strix
  Halo; run via its container). Health via `/health`. Vars: `url`
  (default `http://127.0.0.1:34950`).
- **container** — launch any OpenAI-compatible container image via docker or
  podman (e.g. vLLM, halogen). Vars: `image` (required), `runtime`,
  `containerPort`, `healthPath`, `apiPath`, `mount`, `gpu`, `cmd`.

Plugins: drop a JSON descriptor in `~/.llmctl/plugins/` (or use the UI /
`llmctl backends add`) — http or stdio transport, installed from a URL,
GitHub release, or file list. No recompile.

### Models
- Download from any HuggingFace repo (search + file picker in the UI, or
  `llmctl models install <repo> --file <f.gguf>`).
- Direct URLs work too.
- Resumable downloads (HTTP Range), progress shown live in the UI (SSE).
- Multiple models can be installed and run at the same time.
- GGUF metadata viewer: per-model "Meta" shows quant, architecture, trained
  context length, layer count and all header key/values (parsed locally, no
  upload).

### Instances
- Start/stop from UI or CLI; multiple instances (different models,
  different backends, or replicas) run concurrently.
- Per-instance parameters: ctx, ngl (GPU layers), parallel slots, threads,
  url (attach backends).
- Health-gated startup; crashed instances are reaped and marked stopped.
- One-click restart (UI or `POST /api/v1/instances/{id}/restart`) relaunches a
  managed instance with the same id, port and vars; attached instances are
  rejected (restart the external server instead). A "Stop all" action
  (`POST /api/v1/instances/stop-all`) stops every managed instance (F072).
- Logs per instance under `~/.llmctl/logs/<instance>/`, viewable in the UI
  with one-click full-file download (F075).

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
| `POST /api/v1/instances/{id}/restart` | stop + relaunch with same id/port/vars (managed only) |
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
- Replicas of one model are load-balanced round-robin, and a replica whose
  connection fails is retried on the next replica automatically (F070).

### Web UI
Embedded SPA (no CDN deps — works offline). Dark by default with a header
toggle for a light theme (choice remembered). Tabs:
- **Overview** — status counts, detected GPUs with VRAM usage, disk usage
  (model bytes + free space), running instances, active jobs.
- **Backends** — install/uninstall built-ins, add plugin backends (JSON).
- **Models** — HuggingFace search + file picker, download with live progress.
  Aliases map stable names (e.g. `chat`) to model ids so gateway clients
  survive model swaps; the gateway advertises them in /v1/models.
- **Instances** — start (with ctx/ngl/parallel/threads/url vars) / stop.
- **Chat** — pick a running model, enter/generate an API key, send messages,
  stream tokens live (SSE), multi-turn history. Assistant messages carry real
  token usage from the upstream usage chunk (shown per message and as a
  conversation total); other messages fall back to an estimate
  (~4 chars/token); the conversation shows a context-window bar
  (used vs. model limit). When context reaches 98% of the model's limit,
  older messages are auto-trimmed (system prompt and recent turns kept).
  In agent mode an optional "Ask before tool calls" gate shows each pending
  tool call (name + arguments) inline and runs it only after you approve
  (F055). A collapsible "System prompt" editor sets a per-conversation
  system prompt sent with every request (F063). The last reply can be
  regenerated in place, and the last user message can be pulled back into
  the input for editing and resending (F064). Replies render as markdown
  (built-in safe renderer: code fences, lists, links, emphasis) and every
  message has a hover copy button (F067).
- **Structured output** — give a prompt and a JSON Schema; llmctl asks the
  model for JSON only, validates the reply against the schema and retries
  with the validation error appended until it matches (or gives up with the
  raw reply). REST: POST /api/v1/structured. Dedicated Structured page.
- **Batch inference** — paste prompts (one per line) and run them all
  concurrently through the gateway; per-prompt results and errors in input
  order. REST: POST /api/v1/batch. Dedicated Batch page.
- **Usage** — per-model token bar chart (from /api/v1/metrics) and
  per-conversation token table (from /api/v1/chats). Shows prompt/completion
  totals, request counts, per-model latency percentiles (p50/p95), and
  per-conversation context percentage.
- **Requests** — the last 256 gateway requests (time, model, key, status,
  latency, tokens) in an auto-refreshing table, with one-click CSV export and
  combinable filters (model, status class, min latency, stream/plain).
  REST: GET /api/v1/requests?model=&status=&minMs=&stream=.
- **Tools** — install agent tools from a URL (.zip) or a local folder
  (`~/.llmctl/tools/<id>/`, described by `llmctl-tool.json` with a Python
  `def run(args)` entry). Tools run inside the llm-sandbox framework
  (container, no network, memory & PID caps) by default; a per-tool toggle
  opts out to host execution. List, enable/disable, test-run with args, and
  uninstall. Enabled tools join the agent's tool set in Chat.
- **MCP servers** — add Model Context Protocol servers launched as local
  processes (command + args). llmctl performs the MCP handshake, lists each
  server's tools, and offers them to the agent as
  `mcp:<server>:<tool>`. The MCP page supports add / enable / test (shows the
  advertised tools) / delete. REST: /api/v1/mcp.
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
- **Model comparison** — send the same prompt to multiple running instances
  simultaneously and view responses side by side on the Compare page.
  Uses the existing /v1/chat/completions SSE endpoint; no new backend API needed.
- **Export/import conversations** — GET /api/v1/chats/{id}/export?format=json|markdown
  downloads a conversation as JSON or Markdown. POST /api/v1/chats accepts a
  conversation JSON body for import. Frontend: Export JSON/MD buttons per
  conversation, Import file-upload button in the Conversations card.
- **Prompt templates** — curated built-in prompt templates (code review,
  explain, summarize, translate, brainstorm, critique, debate, rewrite) stored
  in internal/prompt/templates.go. REST API: GET /api/v1/prompts,
  GET /api/v1/prompts/categories, GET /api/v1/prompts/{id}. Dedicated Prompts
  page with category filtering and copy-to-clipboard.
- **File upload** — upload text-based files stored at
  `~/.llmctl/files/<id>/`. REST API: POST /api/v1/files (multipart),
  GET /api/v1/files, GET /api/v1/files/{id}/content, DELETE /api/v1/files/{id}.
  In chat, `/file <filename>` injects file content as a user message.
  Dedicated Files page for upload/list/delete. Text files are stored as-is;
  PDF and DOCX uploads have their text extracted automatically (F053).
- **Cost tracking** — track token usage and estimate costs per API key and per
  model. REST API: GET /api/v1/cost (summary), GET /api/v1/cost/keys,
  GET /api/v1/cost/models. Dedicated Cost page with per-key and per-model
  tables showing token counts and estimated USD costs.
- **Scheduled tasks** — cron-scheduled tasks of three kinds: `chat` (send a
  prompt to a model, result saved as a conversation), `instance.start` and
  `instance.stop` (start/stop an instance on schedule — e.g. load a model
  every morning, unload it at night). Optionally set a webhook URL: after
  every run llmctl POSTs a JSON summary (status ok/error, result or error)
  to it.
  REST API: GET/POST/GET/PUT/DELETE /api/v1/scheduler,
  POST /api/v1/scheduler/{id}/enabled. Dedicated Scheduler page.
- **RAG knowledge bases** — create/delete/edit knowledge bases at
  ~/.llmctl/knowledge/<kb_id>/ with kb.json manifest. Add/remove documents
  (text stored as-is; PDF/DOCX text extracted automatically, F053) via REST
  API. In chat, /file <filename> or KB selection injects
  document content as context. Dedicated Knowledge Bases page. Search ranks
  chunks by TF-IDF cosine similarity, or by embedding similarity when the KB
  is pointed at any OpenAI-compatible `/v1/embeddings` endpoint (per-KB
  semantic-search panel in the UI; TF-IDF remains the fallback).
- **API Keys** — generate / list / revoke. Optional per-key token quota
  (total prompt+completion tokens); once spent, the gateway returns 429
  until the quota is raised. Usage counters persist across restarts. Keys
  page shows usage.
- **Idle auto-unload** — Settings → "Idle auto-unload" (minutes, 0 = off):
  running instances that serve no gateway request for that long are stopped
  automatically, freeing VRAM for other models. Instances page shows each
  instance's last-used time.
- **Notify webhook** — Settings → "Notify webhook": a URL that receives a
  JSON POST when an instance crashes or is idle-unloaded (clean stops stay
  silent).
- **Backup & Restore** — Settings page downloads the whole state (config,
  keys, chats, knowledge bases, memory, skills, tools, scheduler, plugins) as
  one .tar.gz and restores it back; also `llmctl backup [file]` /
  `llmctl restore <file>` on the CLI. Binaries/models/logs are excluded.
  A scheduler task of kind "backup" can snapshot the state automatically on a
  cron (kept in `~/.llmctl/backups`, pruned to the newest N); the Settings
  page lists those snapshots for download or deletion.

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
llmctl restart <instance>
llmctl logs <instance> [--stream stderr|stdout] [--tail N]
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
Docker image, per-key quotas, curated catalog.
