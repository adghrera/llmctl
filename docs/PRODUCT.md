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
- Register a model file already on disk (Models → "Register local model", or
  `POST /api/v1/models` with id + path + backend) — no download needed.
- Resumable downloads (HTTP Range), progress shown live in the UI (SSE).
- Multiple models can be installed and run at the same time.
- GGUF metadata viewer: per-model "Meta" shows quant, architecture, trained
  context length, layer count and all header key/values (parsed locally, no
  upload).

### Instances
- Start/stop from UI or CLI; multiple instances (different models,
  different backends, or replicas) run concurrently.
- Per-instance parameters: ctx, ngl (GPU layers), parallel slots, threads,
  url (attach backends). Editable after the fact with the "Vars" button
  (running instances apply changes on restart).
- Health-gated startup; crashed instances are reaped and marked stopped.
- Crash auto-restart (per-instance "Auto" toggle): a crashed managed process
  is relaunched after a short delay, at most 3 times per 5 minutes before
  giving up; clean stops never trigger it.
- One-click restart (UI or `POST /api/v1/instances/{id}/restart`) relaunches a
  managed instance with the same id, port and vars; attached instances are
  rejected (restart the external server instead). A "Stop all" action
  (`POST /api/v1/instances/stop-all`) stops every managed instance (F072).
- Attached instances are health-probed every 30 s; if the external server
  stops answering, the instance is marked crashed after 3 failed probes and
  the notify webhook fires (F096).
- Start on demand (Settings toggle, F100): with `startOnDemand` on, a gateway
  request for a model that has an auto-start declaration (`llmctl autostart
  add`) but no running instance cold-starts it; the request waits for the
  backend to become healthy. Concurrent requests for the same cold model
  start it only once.
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
- Replicas of one model are load-balanced least-loaded-first (F113), and a
  replica whose connection fails is retried on the next replica
  automatically (F070). The Instances page shows each instance's in-flight
  request count. An optional per-instance "max parallel" cap (Vars dialog)
  sheds excess requests with 503 instead of overloading a backend (F114).

### Web UI
Embedded SPA (no CDN deps — works offline). Dark by default with a header
toggle for a light theme (choice remembered). Tabs:
- **Overview** — status counts, detected GPUs with VRAM usage, disk usage
  (model bytes + free space), running instances, active jobs, and a recent
  events feed (instance crashes, auto-restarts, idle unloads;
  `GET /api/v1/events`, F099) with a kind filter (`?kind=`, F116; also
  `llmctl events -kind …`).
- **Backends** — install/uninstall built-ins, add plugin backends (JSON).
- **Models** — HuggingFace search + file picker, download with live progress.
  Aliases map stable names (e.g. `chat`) to model ids so gateway clients
  survive model swaps; the gateway advertises them in /v1/models. Optional
  per-model USD pricing (`llmctl models price <id> <in> <out>`) drives the
  per-key cost estimates (F102).
- **Instances** — start (with ctx/ngl/parallel/threads/url vars) / stop. Each
  instance can carry a free-text note (Vars dialog) shown under its id and
  persisted across restarts (F119); running instances show their uptime (F125).
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
  totals, request counts, per-model latency percentiles (p50/p95/p99) and
  4xx/5xx error split, average
  time-to-first-token and completion throughput (tok/s), and
  per-conversation context percentage. A "Daily History" table shows the
  per-day, per-model rollup (requests, errors, tokens, estimated cost) which
  persists across restarts (`GET /api/v1/usage/daily[?days=N]`, F097, F103)
  and can be exported as CSV (F117). A parallel per-key rollup
  (`GET /api/v1/usage/keys[?days=N][&key=ID]`) attributes daily requests,
  tokens and cost to each API key (F126).
- **Requests** — the last 256 gateway requests (time, model, serving instance,
  key, status, latency, tokens) in an auto-refreshing table, with one-click
  CSV export and combinable filters (model, status class, min latency,
  stream/plain, key prefix or id). The serving replica is also echoed to clients as
  the `X-LLMCtrl-Instance` response header (F115).
  Each request has an id echoed as the X-Request-Id response header (a
  client-sent X-Request-Id is reused). Keys with a token or request quota get
  X-RateLimit-Limit/-Remaining and X-LLMCtrl-Requests-Limit/-Remaining
  response headers showing what is left (F108).
  History persists across daemon restarts (`~/.llmctl/requests.jsonl`,
  trimmed to the newest 1000 lines at startup).
  REST: GET /api/v1/requests?model=&status=&minMs=&stream=&key= and
  GET /api/v1/requests/{id} for a single request by its id (F104, F106).
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
  every morning, unload it at night), plus `backup` (state snapshot).
  Tasks can be triggered on demand ("Run now" / `llmctl scheduler run`).
  Optionally set a webhook URL: after
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
  until the quota is raised. Usage counters persist across restarts and are
  tracked per key id, so keys that share a visible prefix never share usage
  (F109). Keys page shows usage and estimated cost and links each key to its
  recent requests (request log filtered by key id, F120). Optional per-key model
  allowlist
  (403 for other models; aliases resolved before checking) and concurrency
  cap (429 while more than N requests are in flight). Optional per-key total
  request cap (429 `request_quota_exceeded` once reached; only served
  requests count; F105). Per-key usage counters (tokens, requests, cost) can
  be zeroed with a "Reset" button, `llmctl key reset-usage <id>` or
  `POST /api/v1/keys/{id}/reset-usage` (F107).
- **Idle auto-unload** — Settings → "Idle auto-unload" (minutes, 0 = off):
  running instances that serve no gateway request for that long are stopped
  automatically, freeing VRAM for other models. Instances page shows each
  instance's last-used time.
- **Startup timeout** — Settings → "Startup timeout" (seconds, 0 = default
  240): how long an instance start waits for the backend to become healthy.
  Raise for very large models on slow storage.
- **Max request body** — Settings → "Max request body" (MiB, 0 = default 64):
  gateway requests with a larger body are rejected with 413 (F121).
- **Notify webhook** — Settings → "Notify webhook": a URL that receives a
  JSON POST when an instance crashes or is idle-unloaded (clean stops stay
  silent). A "Test" button sends a test event to verify the endpoint. An
  optional signing secret makes every webhook POST (events, test, scheduler
  task results) carry `X-LLMCtrl-Signature: sha256=HMAC(secret, body)` so
  receivers can verify the sender (F111).
- **CORS** — Settings → "CORS (gateway)": when ON, the gateway answers
  cross-origin preflights (OPTIONS → 204) and sends Access-Control-Allow-*
  headers so browser apps can call /v1/* directly.
- **Backup & Restore** — Settings page downloads the whole state (config,
  keys, chats, knowledge bases, memory, skills, tools, scheduler, plugins) as
  one .tar.gz and restores it back; also `llmctl backup [file]` /
  `llmctl restore <file>` on the CLI. Binaries/models/logs are excluded.
  A scheduler task of kind "backup" can snapshot the state automatically on a
  cron (kept in `~/.llmctl/backups`, pruned to the newest N); the Settings
  page lists those snapshots for download or deletion, and "Create snapshot
  now" writes one on demand (`POST /api/v1/backups`).

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
llmctl key generate|list|revoke   (list shows usage, cost and quota progress)
llmctl scheduler list|add|run|enable|disable|delete
llmctl requests [--model ID] [--status 4|404] [--min-ms N] [-n 20]
llmctl backups [list|create|delete <name>]
llmctl usage [-n 14]
llmctl metrics
llmctl events [-n 20]
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
