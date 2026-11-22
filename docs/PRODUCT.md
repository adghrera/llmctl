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
  `POST /api/v1/models` with id + path + backend) — no download needed. A
  "Dup" button pre-fills that form from any existing model to register a
  variant under a new id (F158). Removing a model deletes its file only when
  llmctl downloaded it; registered local files stay on disk unless removal is
  explicitly forced (`--file` / `?file=true`) (F162).
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
  A restart that passes no `-var` reuses the previous instance's start vars,
  so a crashed attach instance re-attaches to its stored url (F263); the last
  successful start vars are also remembered on the model itself, so `start`
  after an explicit stop (which deletes the instance record) re-attaches with
  the same config instead of falling back to backend defaults (F346).
- Crash auto-restart (per-instance "Auto" toggle): a crashed managed process
  is relaunched after a short delay, at most 3 times per 5 minutes before
  giving up; clean stops never trigger it.
- One-click restart (UI or `POST /api/v1/instances/{id}/restart`) relaunches a
  managed instance with the same id, port and vars; attached instances are
  rejected (restart the external server instead). A "Stop all" action
  (`POST /api/v1/instances/stop-all`) stops every managed instance (F072),
  and a "Restart all" action restarts every managed instance, the UI twin of
  `llmctl restart --all` (F446).
- Attached instances are health-probed every 30 s; if the external server
  stops answering, the instance is marked crashed after 3 failed probes and
  the notify webhook fires (F096).
- Start on demand (Settings toggle, F100): with `startOnDemand` on, a gateway
  request for a model that has an auto-start declaration (`llmctl autostart
  add`) but no running instance cold-starts it; the request waits for the
  backend to become healthy. Concurrent requests for the same cold model
  start it only once. `llmctl autostart export [-out F]` / `autostart import
  FILE` move these declarations between instances (F329);
  `autostart list --csv` exports name,model,backend,port,vars (`;`-joined k=v) (F390),
  and the Instances page auto-start card has matching Export CSV /
  Export JSON / Import JSON buttons (F400, F414).
  and the Instances page auto-start card has a matching Export CSV button (F400).
- Logs per instance under `~/.llmctl/logs/<instance>/`, viewable in the UI
  with one-click full-file download (F075).

### API keys
- Generate in UI or CLI; shown once, stored as sha256 hash.
- Gateway auth (require API key on /v1/*) is OFF by default on a fresh install — chat works without a key.
- Turn it ON in Settings → Require API Key (or start with `--api-key K`) to reject requests without a valid key (401).
- Revoke keys anytime on the API Keys page.
- CLI gateway commands (`chat`, `compare`, `bench`, …) take the key via `-key`
  or, by default, the `LLMCTL_API_KEY` environment variable (F327).

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
- Per-client rate limit + global in-flight cap. The fallback per-key rate
  (used when a key has no own limit, and for unauthenticated traffic keyed on
  IP) is adjustable at runtime from Settings or `PATCH /api/v1/settings`
  (`rateRPS`/`rateBurst`, 0 = CLI defaults) and persists across restarts (F152).
   Rate-limited 429s carry `X-RateLimit-Limit`/`-Remaining` and `Retry-After`
   so clients can back off precisely (F169).
- Replicas of one model are load-balanced least-loaded-first (F113), and a
  replica whose connection fails is retried on the next replica
  automatically (F070). The Instances page shows each instance's in-flight
  request count and has a Chat button that deep-links to the Chat page with
  that instance's model preselected (`/chat?model=`, F362) and an Export CSV
  button matching `llmctl ps --csv` (F406). Its log viewer (stderr/stdout
  tails, download) has a Follow toggle that keeps tailing new lines every 2s,
  the UI counterpart of `llmctl logs -f` (F422), and a filter box that shows
  only lines containing a substring (F450). An optional
  per-instance "max parallel" cap (Vars dialog)
  sheds excess requests with 503 instead of overloading a backend (F114).

### Web UI
Embedded SPA (no CDN deps — works offline). Dark by default with a header
toggle for a light theme (choice remembered). Tabs:
- **Overview** — status counts, a Health card showing only failing doctor
  checks (or "all N checks passed", F241; the full list with a re-run button
  lives on the Settings page, F253), a "Today (UTC)" usage card (requests, errors,
  tokens, estimated cost from the daily rollup, F134), detected GPUs with VRAM
  usage, disk usage
  (model bytes + free space), running instances, active jobs (the Recent jobs
  card has an Export CSV matching `llmctl jobs --csv`, F433), and a recent
  events feed (instance crashes, auto-restarts, idle unloads;
  `GET /api/v1/events`, F099) with kind and model filters (`?kind=`, `?model=`,
  F116, F351; also `llmctl events -kind … -model …`) and an Export CSV button
  matching `llmctl events --csv` columns (F371). A "Daemon log" card shows the tail of the
   daemon's own log on demand (F173) with a Follow toggle that keeps tailing it
  every 2s (UI counterpart of `llmctl logs -daemon -f`, F423) and a filter box
  for substring line filtering (F450). `llmctl events -watch N` live-tails
   the feed, printing new events every N seconds (F187).
- **Search** — one box across local resources (F283): fan-out over the
  existing endpoints to find models (id/backend/path), saved chats (full-text
  with snippets), memory entries, knowledge bases, prompt templates
  (F318) and instances + API keys (F364), each result linking to its page.
  Ctrl+K (Cmd+K on macOS) jumps to Search from any page (F439).
  `llmctl chats search` /
  `memory search` cover the CLI side (F267).
- **Backends** — install/uninstall built-ins, add plugin backends (JSON),
  Export CSV on the list (F404).
- **Models** — HuggingFace search + file picker, download with live progress.
  The installed-models table has a live text filter (id/backend/path, F332)
  and an Export CSV button for the filtered list (same columns as
  `llmctl models list --csv`, F377).
  A Rename button (and `llmctl models rename`) changes a model id, updating
  aliases and stored instance records (F284, F286).
  Aliases map stable names (e.g. `chat`) to model ids so gateway clients
  survive model swaps; the gateway advertises them in /v1/models. Optional
  per-model USD pricing (`llmctl models price <id> <in> <out>`, or the
   Models page "Price $/M" column, F137) drives the
  per-key cost estimates (F102). A "Today" column shows each model's request
   count and estimated cost for the current UTC day (F160).
- **Instances** — start (with ctx/ngl/parallel/threads/url vars) / stop. Each
  instance can carry a free-text note (Vars dialog) shown under its id and
  persisted across restarts (F119); running instances show their uptime (F125)
  and link to their gateway request history (F132); the Req button shows how
  many requests the instance has served since daemon start (F150). Named start
  presets (model + vars + port recipe) can be saved and one-click started from
  the page, and from the CLI with `llmctl presets list|save|start|delete|export|import` (F265, F328);
  `presets list --csv` exports name,model,instance,backend,port,vars (`;`-joined k=v, raw values) (F386),
  and the Instances page Saved-configs card has matching Export CSV /
  Export JSON / Import JSON buttons (F399, F413).
- **Chat** — pick a running model, enter/generate an API key, send messages,
  stream tokens live (SSE), multi-turn history. A "Copy cURL" button in the
  sampling options copies a ready-to-run curl for the conversation's next
  request (model, messages, system prompt, sampling, $LLMCTL_API_KEY, F434). Assistant messages carry real
  token usage from the upstream usage chunk (shown per message and as a
  conversation total); other messages fall back to an estimate
  (~4 chars/token); the conversation shows a context-window bar
  (used vs. model limit). When context reaches 98% of the model's limit,
  older messages are auto-trimmed (system prompt and recent turns kept).
  In agent mode an optional "Ask before tool calls" gate shows each pending
  tool call (name + arguments) inline and runs it only after you approve
  (F055). A collapsible "System prompt" editor sets a per-conversation
  system prompt sent with every request (F063), and the same panel holds
  optional max-tokens/temperature inputs for the conversation (F360). The last reply can be
  regenerated in place, and the last user message can be pulled back into
  the input for editing and resending (F064). Replies render as markdown
  (built-in safe renderer: code fences, lists, links, emphasis) and every
  message has a hover copy button (F067).
- **Structured output** — give a prompt and a JSON Schema; llmctl asks the
  model for JSON only, validates the reply against the schema and retries
  with the validation error appended until it matches (or gives up with the
  raw reply). REST: POST /api/v1/structured. Dedicated Structured page with
  optional max-tokens/temperature inputs (F359) and a Result Export JSON
  button (F432).
- **Batch inference** — paste prompts (one per line) and run them all
  concurrently through the gateway; per-prompt results and errors in input
  order. REST: POST /api/v1/batch. Dedicated Batch page with optional
  max-tokens/temperature inputs (F359) and a Results Export CSV button
  (index,prompt,ok,content,error, F424). CLI:
  `llmctl batch -model ID [-file F|piped stdin|prompts…] [-key K] [-json]
  [-out FILE]` (F224; -out writes the raw JSON to a file, F449).
- **Usage** — per-model token bar chart (from /api/v1/metrics) and
  per-conversation token table (from /api/v1/chats). Shows prompt/completion
  totals, request counts, per-model latency percentiles (p50/p95/p99) and
  4xx/5xx error split, average
  time-to-first-token and completion throughput (tok/s), and
  per-conversation context percentage. A "Daily History" table shows the
  per-day, per-model rollup (requests, errors, tokens, estimated cost) which
  persists across restarts (`GET /api/v1/usage/daily[?days=N]`, F097, F103)
  and can be exported as CSV (F117); a model text filter narrows the table
  (and its CSV) to models whose id contains the text (F352); a "Trend" card
  charts the last 14 days
  of requests and cost as bar charts (F147). A parallel per-key rollup
  (`GET /api/v1/usage/keys[?days=N][&key=ID]`) attributes daily requests,
  tokens and cost to each API key (F126); the Usage page shows it as a
  "Daily by Key" table with its own CSV export (F129). Rollups are pruned to
   the last `usageRetentionDays` UTC days (Settings → "Usage retention";
   default 90, -1 = keep forever) (F168).
- **Requests** — the last 256 gateway requests (time, model, serving instance,
  key, status, latency, tokens) in an auto-refreshing table with a summary bar
  (count, errors, p50/p95 latency, tokens, cost — the UI twin of
  `llmctl requests -stats`, F258) with its own Export CSV (F421), with one-click
  CSV export, per-row expand to full JSON detail (F161), and combinable filters (model, status class or exact code like
  404/429 (F153), min latency,
  stream/plain, key prefix or id, and a since/until time range (F189). The serving replica is also echoed to clients as
  the `X-LLMCtrl-Instance` response header (F115). Authenticated requests also
  echo the serving key's id as the `X-LLMCtrl-Key` response header (F233).
  Each request has an id echoed as the X-Request-Id response header (a
  client-sent X-Request-Id is reused). Keys with a token or request quota get
  X-RateLimit-Limit/-Remaining and X-LLMCtrl-Requests-Limit/-Remaining
  response headers showing what is left (F108).
  History persists across daemon restarts (`~/.llmctl/requests.jsonl`,
  trimmed to the newest 1000 lines at startup). Each entry also carries the
  estimated USD cost from per-model pricing, shown as a Cost column and
  exported in the CSV (F142); failed entries also carry the short error
  reason (e.g. `model "x" not served…`) under the status badge (F151).
  REST: GET /api/v1/requests?model=&status=&minMs=&stream=&key=&instance= and
  GET /api/v1/requests/{id} for a single request by its id (F104, F106).
- **Tools** — install agent tools from a URL (.zip) or a local folder
  (`~/.llmctl/tools/<id>/`, described by `llmctl-tool.json` with a Python
  `def run(args)` entry, or a `command` entry that reads its args JSON on stdin
  and prints the result on stdout, F339). Tools run inside the llm-sandbox framework
  (container, no network, memory & PID caps) by default; a per-tool toggle
  opts out to host execution. List, enable/disable, test-run with args, and
  uninstall. Enabled tools join the agent's tool set in Chat. The Tools page
  has an Export CSV button on the installed list (F396).
- **MCP servers** — add Model Context Protocol servers launched as local
  processes (command + args). llmctl performs the MCP handshake, lists each
  server's tools, and offers them to the agent as
  `mcp:<server>:<tool>`. The MCP page supports add / enable / test (shows the
  advertised tools) / delete, plus Export CSV on the server list (F405). REST: /api/v1/mcp.
- **Skills** — create reusable prompt templates invoked in chat via
  `/s.<short_name> <text>`. Each skill has a short name, display name,
  description, prompt template (with {{input}} and {{conversation}} placeholders),
  tags, and an enabled flag. Stored as directories at
  `~/.llmctl/skills/<id>/`. The Skills page supports add/edit/remove/enable/disable
  and an Export CSV button on the installed list (F395), per-skill Export JSON
  (manifest, CLI parity) and an Import JSON button on the list (F415).
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
  injected as system context. Dedicated Memory page to add/delete entries,
  with an Export CSV button on the entry list (F402) and a per-entry Copy
  button for the content (F441).
- **Model comparison** — send the same prompt to multiple running instances
  simultaneously and view responses side by side on the Compare page, with
  optional max-tokens/temperature inputs (F359) and a Results Export JSON
  button matching `llmctl compare --json` shape (F431).
  Uses the existing /v1/chat/completions SSE endpoint; no new backend API needed.
  CLI: `llmctl compare -models A,B <prompt>` streams the replies in sequence (F301);
  add `-json` for a machine-readable `[{model, reply|error}]` array (F314),
  or `-out FILE` to write that array to a file (F454).
- **Export/import conversations** — GET /api/v1/chats/{id}/export?format=json|markdown
  downloads a conversation as JSON or Markdown. POST /api/v1/chats accepts a
  conversation JSON body for import. Frontend: Export JSON/MD buttons per
  conversation, a Rename button (F296) that edits the title in place, and an
  Import button in the Conversations header that uploads an export file
  verbatim (tools/system included) under a fresh id and opens it (F369,
  plus an "Export CSV" button there downloading the conversation list —
  id,title,model,messages,updated_at, same columns as `chats list --csv`, F392).
  replacing the earlier field-picking section); CLI twin `chats import` (F368).
- **Prompt templates** — curated built-in prompt templates (code review,
  explain, summarize, translate, brainstorm, critique, debate, rewrite) stored
  in internal/prompt/templates.go, plus user-defined custom templates stored
  in state (F306): POST/DELETE /api/v1/prompts, merged into the list with a
  `custom` flag; CLI `prompts add <id> -name N [-content T]` / `prompts update <id>
  [-name N …]` / `prompts delete <id>`;
  the Prompts page has a New-template form and per-custom-entry Edit/Delete (F307, F309)
  plus a live text filter with `?q=` deep links from Search (F322) and an
  Export CSV button on the template list (F394);
  `prompts export [-out F]` / `prompts import FILE` move custom templates
  between instances (F323); the Prompts page has matching Export JSON /
  Import JSON buttons (F409).
  REST API: GET /api/v1/prompts,
  GET /api/v1/prompts/categories, GET /api/v1/prompts/{id}. Dedicated Prompts
  page with category filtering, copy-to-clipboard, an "Insert into chat"
  button that opens the Chat page with the template pre-filled (F300), and a
  per-template "Render" preview that fills {{input}} and extra {{k}} vars
  live (F361). CLI:
   `llmctl prompts [list] [filter] [-category X] [-json] [-csv]` (the `list`
   word is accepted as an explicit subcommand, and `--csv` exports the
   filtered list as id,category,name,description, header-only when empty, F382)
   and
   `llmctl prompts show <id> [-out file]` and `llmctl prompts render <id> [-input TEXT |
   piped stdin] [-var k=v …] [-out file]` (renders the template with {{input}} and any
   {{k}} placeholders filled, for scripts; -out writes the rendered text to a file, F442)
   and `llmctl prompts categories` (lists categories, one per line)
   (F218, F298, F304).
 - **Chat CLI** — `llmctl chats [filter] [-json]` (or `chats list [filter]`)
   lists saved conversations (`--csv` exports the list, F378),
   `llmctl chats show <id> [-json] [-last N] [-grep TEXT] [-full]` prints one
   with a message preview,
   `llmctl chats search <query> [-json]` full-text searches message content
   with match snippets, `chats export <id> [-md] [-out f]` and
   `chats export-all [-out DIR] [-md]` (bulk file export, F325),
   `chats import <file.json>` re-imports an exported chat under a fresh id
   (F368),
   `chats fork <id> [-at N] [-title T]`, `chats rename <id> <title>`,
   `chats pin|unpin <id>` (pinned chats sort first, F310; the Chat page
   shows a 📌 marker and a Pin/Unpin button, F311),
   `chats delete <id>`, `chats prune -before D [-older-than N] [-yes]`
   (bulk-delete old chats, pinned skipped, dry-run by default, F333), and
   `chats delete-all -yes` manage them
   (over the chat-store API, F244, F267, F269, F295, F299, F310).
 - **Skills CLI** — `llmctl skills [filter] [-json] [-csv]` lists skill packs
   (filter matches id/name/description/tags; `--csv` exports
   id,enabled,name,description,tags with tags joined by `;`, header-only
   when empty, F384); `skills show|enable|disable|delete <id>`
   manages them, `skills add <manifest.json>` creates one from a JSON
   manifest and `skills export <id> [-out f]` dumps one back out (F245, F288, F305).
 - **Memory CLI** — `llmctl memory [filter] [-json] [-csv]` lists memory entries
   (`--csv` exports id,created_at,tags (`;`-joined),content, F401);
   `memory add "<text>" [-tags a,b]`, `memory update <id> [-content C]
   [-tags a,b]`, `memory show <id>`, `memory search <query> [-json]`,
   `memory delete <id>`; `memory export [-out F]` / `memory import FILE`
   move entries between instances (F246, F267, F289, F321); the Memory page
   has matching Export JSON / Import JSON buttons (F410).
 - **KB CLI** — `llmctl kb [filter] [--csv] [-json]` lists knowledge bases;
   `kb create <name> [-desc X]`, `kb update <id> [-name N] [-desc D]`,
   `kb show <id> [-json]` (human view: description, doc list, search mode —
   embeddings vs TF-IDF — F366), `kb docs|delete <id>`, `kb doc <kb-id> <doc-id> [-out F]` (download a
   document's text, F320) and
   `kb add-doc <kb-id> <path> [-name X]` upload documents,
   `kb export <kb-id> [-out DIR]` dumps all documents as files (F324) and
   `kb search <kb-id> <query> [-limit N] [-json]` runs RAG search and
   `kb embed <kb-id> -url U -model M [-key K] | -clear` configures semantic
   search (F247, F250, F268, F272, F291, F320).
 - **Tools CLI** — `llmctl tools [-json] [-csv]` lists installed tools
   (`--csv` exports id,enabled,sandbox,runtime,name,description, F389);
   `tools install <dir>` installs a local pack, `tools show|enable|disable|delete <id>`
   and `tools run <id> -args '<json>'` (or `-args -` for stdin / `-args-file F`;
   `-out FILE` writes the result to a file, F458)
   execute one from the shell (F248, F250, F338).
 - **MCP CLI** — `llmctl mcp [-json] [-csv]` lists configured MCP servers
   (`--csv` exports id,name,enabled,command, F403);
   `mcp add <name> <command> [-args 'a b']`, `mcp remove|enable|disable <id>`,
   `mcp test <id> [--csv|--json] [-out FILE]` (connect and list advertised tools;
   --csv/--json export name,description, F436) and
   `mcp export [-out F]` / `mcp import FILE` (configs move between instances,
   enabled state preserved, F330) (F249); the MCP page has matching
   Export JSON / Import JSON buttons (F411) and an Export CSV on the tested
   server's Tools card with the same columns (F437).
 - **Cost CLI** — `llmctl cost` prints lifetime token/cost totals and a
   per-model table; `-keys` shows per-key costs with names, `-model M` /
   `-key K` filter the table to one id, `--csv` exports the table, `-json` emits
   `{totals, groups}` (F251, F349, F370).
 - **File CLI** — `llmctl files [list|upload <path> [-name X]|cat <id>|delete
   <id>] [--csv] [-json]` covers the whole uploaded-files API from the shell, including
   uploads (F219, F243) and CSV inventory export (F381).
- **File upload** — upload text-based files stored at
  `~/.llmctl/files/<id>/`. REST API: POST /api/v1/files (multipart),
  GET /api/v1/files, GET /api/v1/files/{id}/content, DELETE /api/v1/files/{id}.
  In chat, `/file <filename>` injects file content as a user message.
  Dedicated Files page for upload/list/delete, with a filename text filter
  (F356), an Export CSV button (filter-aware, F397), and drag-and-drop upload —
  drop a file onto the upload card to upload it directly (F452). Text files are stored as-is;
  PDF and DOCX uploads have their text extracted automatically (F053).
- **Cost tracking** — track token usage and estimate costs per API key and per
  model. REST API: GET /api/v1/cost (summary), GET /api/v1/cost/keys,
  GET /api/v1/cost/models. Dedicated Cost page with per-key and per-model
  tables showing token counts and estimated USD costs, each with a text
  filter on the id (F353) and a filter-aware Export CSV button (F417).
- **Scheduled tasks** — cron-scheduled tasks of three kinds: `chat` (send a
  prompt to a model, result saved as a conversation), `instance.start` and
  `instance.stop` (start/stop an instance on schedule — e.g. load a model
  every morning, unload it at night), plus `backup` (state snapshot).
  Chat tasks can name a knowledge base (`-kb`, Scheduler page dropdown):
  the top matching chunks are injected as context, same as `chat -kb`
  (F365). Scheduled chats reach the gateway with a memory-only internal
  token, so they work even when gateway auth is on (F365).
  Tasks can be triggered on demand ("Run now" / `llmctl scheduler run`).
  `llmctl scheduler export [-out F]` / `scheduler import FILE` move schedule
  specs between instances (fresh ids on import, F326); the Scheduler page has
  matching Export JSON / Import JSON buttons (F412).
   Cron expressions are validated on create/update (5 fields, in-range
   values; invalid → 400) so a typo can’t silently mis-schedule (F159).
  Optionally set a webhook URL: after
  every run llmctl POSTs a JSON summary (status ok/error, result or error)
  to it.
  REST API: GET/POST/GET/PUT/DELETE /api/v1/scheduler,
  POST /api/v1/scheduler/{id}/enabled. Dedicated Scheduler page with a
  "Run" button per task (F141), an "Edit" button (prompts for name/cron/model/
  prompt and PUTs the updated spec, F293) and a "Recent Runs" history of the last 50
  executions — status, duration and error/result excerpt
  (GET /api/v1/scheduler/runs, F145) with an Export CSV button matching
  `scheduler runs --csv` (F418, F419). Each task row also shows its most
   recent run's status (ok/error badge) from that ring, in the Scheduler page
   and `llmctl scheduler list` (F184), plus an "Export CSV" button on the
   task list with the same columns as `scheduler list --csv` (F391).
- **RAG knowledge bases** — create/delete/edit knowledge bases at
  ~/.llmctl/knowledge/<kb_id>/ with kb.json manifest. Add/remove documents
  (text stored as-is; PDF/DOCX text extracted automatically, F053) via REST
  API. In chat, /file <filename> or KB selection injects
  document content as context. Dedicated Knowledge Bases page with an
  Export CSV button on the KB list (F393) and per-KB Export/Import JSON
  (all documents as a [{filename, content}] array, the UI counterpart of
  `llmctl kb export`, F435); dropping a file onto the KB detail card uploads
  it as a document (F453). Search ranks
  chunks by TF-IDF cosine similarity, or by embedding similarity when the KB
  is pointed at any OpenAI-compatible `/v1/embeddings` endpoint (per-KB
  semantic-search panel in the UI; TF-IDF remains the fallback).
- **API Keys** — generate / list / revoke. Optional per-key token quota
  (total prompt+completion tokens); once spent, the gateway returns 429
  until the quota is raised. Usage counters persist across restarts and are
  tracked per key id, so keys that share a visible prefix never share usage
  (F109). Keys page shows usage and estimated cost, today's per-key usage
  (F127), a live text filter (name/id/prefix, F335), an Export CSV button for
  the filtered list (same columns as `llmctl key list --csv`, F375), and links
  each key to its
  recent requests (request log filtered by key id, F120); keys can be renamed
  inline (F140) and carry a free-text note edited inline or via
  `llmctl key note <id> <text>` (F186). `llmctl key update <id>` edits any
  of those later — name, note, models, quota, caps, rate, expiry, budget (F254).
  Optional per-key model
  allowlist (editable via an inline checkbox editor on the Keys page, F154)
  (403 for other models; aliases resolved before checking; restricted keys
   also only see allowed models in `/v1/models`, F177) and concurrency
  cap (429 while more than N requests are in flight). Optional per-key total
  request cap (429 `request_quota_exceeded` once reached; only served
  requests count; F105). Optional per-key rate limit (`rateRPS` sustained
   + `rateBurst`, set at create or PATCH; 0 = gateway default; 429 on excess;
   F130). Keys may expire (`expiresInDays` at create/PATCH, 0 = never; expired
   keys get 403 `key_expired`; F135). Keys can also be temporarily blocked and
    re-enabled without deleting them (`llmctl key disable/enable`, a Disable/
    Enable button on the Keys page, PATCH
    `{"disabled":true}`; blocked keys get 403 `key_disabled`; F195, F205). A key's secret can be
    rotated without losing its id, quotas or usage counters (`llmctl key
    rotate <id>`, a Rotate button on the Keys page, `POST
    /api/v1/keys/{id}/rotate`; the new secret is shown once and the old one
    stops working immediately; F215). Expired keys can be bulk-deleted with
   `llmctl key prune` / `POST /api/v1/keys/prune` or the Keys page "Prune
   expired" button (F167). A per-key daily cost budget
   (`dailyCostBudgetUSD`, 0 = unlimited) returns 429 `budget_exceeded` once
   the key's estimated USD spend for the UTC day reaches it (F138). Per-key usage counters (tokens, requests, cost) can
  be zeroed with a "Reset" button, `llmctl key reset-usage <id>` or
  `POST /api/v1/keys/{id}/reset-usage` (F107). Token and request quotas can
   be changed after creation via PATCH and the Keys page quota editor (F148), and
   each key shows when it was last used (F149). `llmctl key show` and
   `GET /api/v1/keys/{id}` also report lifetime totals (requests, errors,
   tokens, cost) summed over the retained per-key daily rollups (F182).
- **Idle auto-unload** — Settings → "Idle auto-unload" (minutes, 0 = off):
  running instances that serve no gateway request for that long are stopped
  automatically, freeing VRAM for other models. Per-model override on the
  Models page "Idle" column: `-1` pins a model (never unloaded), `>0` sets
  custom minutes even when the global is off (F136). Instances page shows each
  instance's last-used time.
- **Startup timeout** — Settings → "Startup timeout" (seconds, 0 = default
  240): how long an instance start waits for the backend to become healthy.
  Raise for very large models on slow storage.
- **Max request body** — Settings → "Max request body" (MiB, 0 = default 64):
  gateway requests with a larger body are rejected with 413 (F121).
 - **Daemon log level** — Settings → "Daemon log level" (debug/info/warn/error):
   verbosity of `~/.llmctl/llmctl.log`, changeable at runtime and persisted;
   debug adds a per-request trace line on the gateway (F171).
- **Notify webhook** — Settings → "Notify webhook": a URL that receives a
  JSON POST when an instance crashes or is idle-unloaded (clean stops stay
  silent). A "Test" button sends a test event to verify the endpoint. An
  optional signing secret makes every webhook POST (events, test, scheduler
  task results) carry `X-LLMCtrl-Signature: sha256=HMAC(secret, body)` so
  receivers can verify the sender (F111). An "events" filter (comma-separated
  kinds, empty = all) restricts which events are POSTed, e.g. only
  `instance.crashed` (F143). The last 50 delivery attempts (time, event,
  status/error) are listed under the webhook, at
  GET /api/v1/webhook/log (F144), and in `llmctl webhooks` (F191), which
  supports `-n` (server-side `?limit=`) and `-event` (server-side `?event=`)
  filters (F357) and `--csv` export (F380); the Settings page delivery list has a matching event
  dropdown (F358) and a filter-aware Export CSV button (F416). Event and
  scheduler-hook deliveries retry
  up to 3 times (1s, 2s backoff) so a briefly unreachable receiver still gets
  the notification (F163).
- **CORS** — Settings → "CORS (gateway)": when ON, the gateway answers
  cross-origin preflights (OPTIONS → 204) and sends Access-Control-Allow-*
  headers so browser apps can call /v1/* directly.
- **Backup & Restore** — Settings page downloads the whole state (config,
  keys, chats, knowledge bases, memory, skills, tools, scheduler, plugins) as
  one .tar.gz and restores it back; also `llmctl backup [file]` /
  `llmctl restore <file>` on the CLI, and `llmctl backup verify <file>` to
  check any local archive offline (F262). Binaries/models/logs are excluded.
  A scheduler task of kind "backup" can snapshot the state automatically on a
  cron (kept in `~/.llmctl/backups`, pruned to the newest N — per-task `keep`
   or the global `settings.backupKeep`, default 7, F181); the Settings
  page lists those snapshots for download, restore-in-place, integrity
  verification (`llmctl backups verify <name>` / Verify button, F242), download to a file
  (`llmctl backups download <name> [-out f]`, F276) or deletion,
  and
  "Create snapshot now" writes one on demand (`POST /api/v1/backups`; restore via
  `llmctl backups restore <name>` or the Restore button, F175, F176; CLI
  `backups create [-name LABEL]` and a UI label input embed a sanitized label
  in the file name, llmctl-<label>-<ts>.tar.gz, F443, F444), next to an
  Export CSV button matching `backups list --csv` (F398).
  `llmctl backups list --csv` exports the snapshot inventory
  (name,size_bytes,created), header-only when empty (F383).

Live job progress streams over SSE. Chat state (key, draft, conversation)
survives the periodic UI re-render.

## CLI

```
llmctl up [--addr :8080] [--rate 20] [--burst 60] [--max-inflight 512] [--api-key K]
llmctl down
llmctl status [-json] [-watch N]   (-watch N: live refresh; -json: raw; version, uptime, running instances with
                                    uptime, gateway auth, today's usage)
llmctl doctor [-json] [--csv] [-quiet] [-check NAME]...  (--csv exports name,status,detail (F429); -quiet prints only warn/error lines + a summary; -check keeps matching checks, repeatable)
llmctl gpu [-json] [--csv] [-out FILE]             (GPU inventory: vendor, name, used/total; --csv adds raw bytes + used_pct, -out writes the raw JSON, F430, F463)
llmctl jobs [-json] [--csv] [-watch N]              (background install jobs; --csv exports the job list, F427, F273)
llmctl search <query> [-max N] [-json] [--csv] [-out FILE]  (keyless web search; --csv exports rank,title,url,snippet, -out writes the raw JSON, F428, F274, F462)
llmctl structured -model ID -schema f.json [-key K] [-max-tokens N] [-temperature T] [-out FILE] <prompt>   (schema-validated JSON, F279, F344; -out writes it to a file, F457)
llmctl embed -model ID [-key K] [-json] [-out FILE] <text>     (gateway embeddings, F281; -out writes the JSON to a file, F456)
llmctl complete -model ID [-key K] [-json] [-out FILE] <prompt>  (gateway text completions, F287; -out writes the text to a file, F455)
llmctl compare -models A,B [-system S] [-max-tokens N] [-temperature T] [-json] [-out FILE] <prompt> (CLI twin of Compare page, F301, F314, F343; -out writes the JSON array to a file, F454)
llmctl count <text> [-file F] [-chat ID] [-model ID] [-json] [-out FILE]  (estimate tokens, ~4 chars/token; -chat sizes a saved conversation, F317, F336; -out writes the counts JSON to a file, F461)
llmctl bench -model ID [-key K] [-n N] [-conc C]              (latency/throughput benchmark, F282)
llmctl bench -models A,B [-n N] [-conc C] [-json|--csv] [-out FILE]  (multi-model comparison table; --csv one row per model, -out writes the stats JSON to a file, F426, F303, F315, F460)
llmctl disk [-json] [--csv]                        (model dir + volume disk usage; --csv one row, raw bytes, F451)
                                    health sweep: state dir, backends, models,
                                    instances, gateway, backups, security (warns when the gateway is
                                    bound to a public address with auth off, F183), disk (warns on
                                    orphaned files in the models dir, F206), keys (warns on expired
                                    keys, F363), backup age (warns when the
                                    newest snapshot is >7 days old, F207); security also warns when auth
                                    is on but no usable key exists (F223), aliases (warns when an
                                    alias targets a deleted model, F228), webhook (warns when recent
                                    notify-webhook deliveries failed, F232), low disk (state check
                                    warns below 1 GiB free, F236), scheduler (warns when a schedule
                                    references a missing model or recent runs failed, F334);
                                    exit ≠0 on errors)
llmctl ps [-watch N] [--csv] [-json]      (-watch N: live refresh; --csv: one-shot CSV export; -json: raw; also shows uptime, requests served, auto-restart
                                    count and per-instance notes)
llmctl backends list|install|uninstall|add|remove   (`list --csv` exports id,transport,kind,installed,version,description, F388)
llmctl catalog list [-json|-csv] | install <id>      (curated model picker, F266; `list --csv` exports id,name,backend,size_gb,quant,tags, F387)
llmctl presets list [-json|-csv]|save|start|delete|export|import  (named start recipes, F265, F328)
llmctl models list|show|install|remove|hf-search|hf-files   (list shows human sizes + total, --csv for export,
llmctl models add <id> <file> [-backend ID]              (register an existing local GGUF, F275)
llmctl models rename <id> <new-id>                    (change a model id; aliases follow, F284)
                                    price and idle-unload; show also lists aliases pointing at the
                                    model (F367); list/show/aliases/hf-search/hf-files --json for scripts;
                                    remove --file also deletes a registered external file)
llmctl models aliases|alias <name> <id>|unalias <name>   (gateway model aliases, F180; `aliases --csv` exports alias,model, F385)
llmctl models probe [-json]                          (what /v1/models serves now, F252)
llmctl models routable [-key K] [-json|-csv]         (what /v1/models serves for one key, via the gateway — reflects the key's allowlist, F337; --csv exports id,owned_by, F407)
llmctl models ctx <id> <tokens> | price <id> <in/M> <out/M> | idle <id> <min|never>   (model settings, F255)
llmctl models metadata <id> [-json]                     (GGUF header metadata, F268)
llmctl models unused [--csv]                           (models with no instance + reclaimable size, F280; --csv exports id,size_bytes, F408)
llmctl start <model> [--backend id] [--instance name] [--var k=v ...]
llmctl stop <instance> [--force]
llmctl stop --all                          (stop every running instance, attached ones stay, F278)
llmctl restart <instance> | llmctl restart --all   (--all restarts each running instance, reporting per-instance failures, F445)
llmctl maxparallel <instance> <n>   (concurrent-request cap, 0 = unlimited, F270)
llmctl note <instance> <text>            (free-text annotation, F271)
llmctl autorestart <instance> <on|off>   (crash auto-restart toggle, F271)
llmctl setvars <instance> [-var k=v…]    (replace stored start vars, F271)
llmctl logs <instance> [--stream stderr|stdout] [--tail N] [-out FILE] [-f]
                                    (-out FILE: download the full log file, F292)
llmctl logs -daemon [--tail N] [-f]         (daemon's own log, ~/.llmctl/llmctl.log; -f follows it, F355)
llmctl key generate|show|note|update|enable|disable|rotate|list|revoke|prune   (generate: -quota -maxreq -models -expires -budget -rps -note;
                                        list --csv exports the audit table (F374);
                                        generate/rotate -json prints the raw JSON incl. the secret;
                                        show: one key's full view (-json for scripts); prune: -dry-run lists; note: set a free-text note;
                                        list shows usage, cost,
                                        quota, expiry, daily budget, note (--json for scripts); prune deletes expired keys)
llmctl scheduler list|show|add|update|run|runs|enable|disable|delete   (list/runs --json for scripts; list --csv for audit export (F376);
                                    add: -name -cron -kind -model -prompt -instance -webhook -keep -kb;
                                    update: same flags, changes only what you pass, F290)
llmctl requests [--model ID] [--status 4|404] [--min-ms N] [-n 20] [--csv] [--json] [-since 30m|RFC3339] [-until …] [-watch N] [-stats [-by model|key|instance]]   (--csv also exports -stats rows, F420)
llmctl backups [list|create [-name LABEL]|restore <name>|delete <name]]   (list: human sizes + total, --json)
llmctl usage [-n 14] [-key ID | -keys] [-model ID] [--csv] [--json]
llmctl metrics [-watch N] [-model M] [--csv] [-json]  (-watch N: live refresh every N seconds; -model M: filter the table; --csv: one-shot CSV export)
llmctl events [-n 20] [-watch N] [-kind K] [-model M] [-json]
llmctl webhooks [-n 20] [-event E] [--csv] [-json] | webhooks test    recent delivery attempts / send a test event (F447)
llmctl prompts [filter] [-category X] [-json] | prompts show <id> [-out file] | prompts render <id> [-input TEXT] [-var k=v…] [-out file]
llmctl files [list|upload <path>|cat <id>|delete <id>] [--csv] [-json]
llmctl settings [-json] | settings set -require-auth -idle-min -notify-webhook
                 -notify-secret -allow-cors -start-on-demand -max-body-mb
                 -rate-rps -rate-burst -retention-days -log-level -backup-keep
                 | settings test-webhook (send a test event)
llmctl batch -model ID [-system S] [-key K] [-concurrency N] [-temperature T] [-file F|stdin|prompts…] [-json] [-out FILE]
llmctl chat [--model ID] [--key KEY] [--system TEXT]
                                    (-prompt TEXT or piped stdin: one-shot mode — send one message,
                                     print the streamed reply, exit; for scripts, F277;
                                     add -save "Title" to also store the exchange as a saved chat, F294;
                                     add -chat ID to send the saved chat's history and append the reply to it, F297;
                                     add -continue to append to the most recently updated saved chat, F312;
                                     add -json to print {model, prompt, reply, chat_id} as one JSON object, F313;
                                     add -temperature/-max-tokens sampling options, F319;
                                     add -kb ID to prepend the top knowledge-base matches as context, F302;
                                     add -out FILE to write the reply text to a file instead of stdout, F448)
                                    (-agent with -prompt: tool-using agent loop, prints [tool …]
                                     lines and step count, F285)
                                    (interactive REPL: /help /clear /system TEXT /model ID
                                     /kb ID (/kb - detaches) /save [title] /quit, F331, F342)
llmctl version [--json]                            (--json prints {"cli","daemon"}; daemon empty when not running, F440)
llmctl completion [bash|zsh|fish|powershell]         (print a shell-completion script for llmctl + subcommands, F341, F350, F354; powershell adds a Register-ArgumentCompleter for $PROFILE, F425)
```

## Design principles
- Simple over clever: stdlib Go, one binary, one port, one state file.
- Everything the UI does is an API call (agents/CI can use the same surface).
- Pluggable by data (JSON), not by code, for common cases.
- Prefer stdio IPC for backends that support it; HTTP otherwise.

## Not yet (see docs/PLAN.md)
Docker image, per-key quotas, curated catalog.
