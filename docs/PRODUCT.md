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
  upload), with an Export JSON button (twin of `models metadata -json`, F767).

### Instances
- Start/stop from UI or CLI; multiple instances (different models,
  different backends, or replicas) run concurrently.
- Per-instance parameters: ctx, ngl (GPU layers), parallel slots, threads,
  url (attach backends). Editable after the fact with the "Vars" button
  (running instances apply changes on restart), which also has a Copy button for
  the vars JSON (F791).
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
  with `-out FILE` writing the raw JSON (F494) and `autostart add|remove -json`
  printing the raw response (F526, F527),
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
- `GET /v1/models/{id}` (single model/alias retrieve, OpenAI SDK parity;
  honours the per-key allowlist, 404 envelope when not visible, F677)
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
  that instance's model preselected (`/chat?model=`, F362) and Export CSV/JSON
  buttons matching `llmctl ps --csv`/`-json` (F406, F731), plus a Stats toggle rolling the
  list up per model — replicas, running, served, restarts — mirroring
  `llmctl ps -stats` (F635), a tag-filter dropdown twin of `llmctl ps
  -tag` (F708), and clickable column headers to sort the list (F776). Its log viewer (stderr/stdout
  tails, download) has a Follow toggle that keeps tailing new lines every 2s,
  the UI counterpart of `llmctl logs -f` (F422), a filter box that shows
  only lines containing a substring (F450), and a Copy button for the
  displayed tail (F760). An optional
  per-instance "max parallel" cap (Vars dialog)
  sheds excess requests with 503 instead of overloading a backend (F114).

### Web UI
Embedded SPA (no CDN deps — works offline). Dark by default with a header
toggle for a light theme (choice remembered). Tabs:
- **Overview** — status counts, a Health card showing only failing doctor
  checks (or "all N checks passed", F241; the full list with a re-run button
  lives on the Settings page, F253, and both lists export as CSV/JSON, F723, F724, with a
  "Shutdown daemon" button there (twin of `llmctl down`, F771), a "Today (UTC)" usage card (requests, errors,
  tokens, estimated cost from the daily rollup, F134; the requests stat
  deep-links to the Requests page for today's window, F747), detected GPUs with VRAM
  usage and Export CSV/JSON buttons (F721), disk usage
  (model bytes + free space, with Export CSV/JSON buttons, F722), running
  instances, active jobs (the Recent jobs
  card has Export CSV / Export JSON matching `llmctl jobs --csv`/`-json`, F433, F719, a Clear done
  button that drops finished jobs from daemon memory, F586, and a Stats toggle
  rolling the list up per kind (twin of `jobs -stats`, F752); running jobs show
  a live progress bar plus the job message, F597), and a recent
  events feed (instance crashes, auto-restarts, idle unloads;
  `GET /api/v1/events`, F099) with kind, model and key-name filters
  (`?kind=`, `?model=`, `?key=`, F116, F351, F696; also
  `llmctl events -kind … -model … -key …`) and Export CSV / Export JSON buttons
  matching `llmctl events --csv`/`-json` columns (F371, F719). A "Daemon log" card shows the tail of the
   daemon's own log on demand (F173) with a Follow toggle that keeps tailing it
  every 2s (UI counterpart of `llmctl logs -daemon -f`, F423), a filter box
  for substring line filtering (F450), a Download button saving the
  displayed tail (F744) and a Copy button for it (F760), and a Stats toggle that rolls the feed
  up per kind — count, last seen, who (UI twin of `llmctl events -stats`, F600).
  `llmctl events -watch N` live-tails
   the feed, printing new events every N seconds (F187).
- **Search** — one box across local resources (F283): fan-out over the
  existing endpoints to find models (id/backend/path), saved chats (full-text
  with snippets), memory entries, knowledge bases, prompt templates
  (F318) and instances + API keys (F364), plus skills and scheduler tasks
  (F698), each result linking to its page.
  An Export CSV or Export JSON button downloads every match as one
  kind,name,detail file
  (F611, F718). Ctrl+K (Cmd+K on macOS) jumps to Search from any page (F439).
  Pressing `?` anywhere outside a text field opens a keyboard-shortcut
  cheat-sheet overlay; Esc or a backdrop click closes it (F514). A `?`
  button in the header opens the same overlay (F515).
  `llmctl chats search` /
  `memory search` cover the CLI side (F267).
- **Backends** — install/uninstall built-ins, add plugin backends (JSON),
  remove plugin registrations with a per-row Remove button (CLI twin of
  `llmctl backends remove`, F658), Export CSV and Export JSON on the list
  (F404, F715).
- **Models** — HuggingFace search + file picker, download with live progress.
  The installed-models table has a live text filter (id/backend/path, F332),
  a click-to-copy model-id cell, a Chat link on running rows and a Req link to
  the model's request history (F738, F740, F741),
  and a tag dropdown filtering by exact tag (UI twin of `models list -tag`,
  F682), and Export CSV / Export JSON buttons for the filtered list (same columns as
  `llmctl models list --csv`, F377, F718) plus a Stats toggle rolling the filtered
  list up per backend — models, running, size — mirroring `models stats` (F634).
  A Rename button (and `llmctl models rename`) changes a model id, updating
  aliases and stored instance records (F284, F286). A Note button (and
  `llmctl models note <id> [text…]`) attaches a free-text annotation to a
  model — the model twin of instance notes, shown under the model id and in
  `models show` / `models list --csv` (F661). A Tags button (and
  `llmctl models tag <id> a,b` / `models retag` / `models list -tag X`)
  labels models with free-form tags, shown as badges under the id and
  matched by the page filter (F662).
  Aliases map stable names (e.g. `chat`) to model ids so gateway clients
  survive model swaps, with Export CSV/JSON on the Models page Aliases card
  (F726); the gateway advertises them in /v1/models. Optional
  per-model USD pricing (`llmctl models price <id> <in> <out>`, or the
   Models page "Price $/M" column, F137) drives the
  per-key cost estimates (F102). A "Today" column shows each model's request
   count and estimated cost for the current UTC day (F160).
- **Instances** — start (with ctx/ngl/parallel/threads/url vars) / stop. An
  **Auto-refresh** checkbox polls every 5s so status and uptime update without
  a reload — the UI twin of `llmctl ps -watch N` (F612). Each
  instance can carry a free-text note (Vars dialog) shown under its id and
  persisted across restarts (F119); it can also carry free-form tags (Vars
  dialog, `llmctl tag <instance> a,b` / `llmctl retag`), shown under the note
  and persisted like notes (F673); running instances show their uptime (F125),
  have click-to-copy id and Port cells (endpoint URL, F737, F739), and link to
  their gateway request history (F132); the Req button shows how
  many requests the instance has served since daemon start (F150). Named start
  presets (model + vars + port recipe) can be saved and one-click started from
  the page, and from the CLI with `llmctl presets list|save|duplicate|start|delete|export|import` (F265, F328; `duplicate` copies a preset's model/instance/backend/port/vars under a new name, F648);
  `presets list --csv` exports name,model,instance,backend,port,vars (`;`-joined k=v, raw values) (F386),
  and the Instances page Saved-configs card has matching Export CSV /
  Export JSON / Import JSON buttons (F399, F413) and a per-row Duplicate
  button (F649).
- **Chat** — pick a running model, enter/generate an API key, send messages,
  stream tokens live (SSE), multi-turn history. While a reply streams, the
  Send button becomes a Stop button that cancels the request and keeps the
  partial reply (F754). A "Copy cURL" button in the
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
  system prompt sent with every request (F063), persisted in the browser
  across reloads (F762), and the same panel holds
  optional max-tokens/temperature inputs for the conversation (F360), also
  persisted in the browser (F763), plus a seed input for reproducible replies
  passed through to the backend (F793), a top_p nucleus-sampling input
  (F797) and frequency/presence penalty inputs (F803). The last reply can be
  regenerated in place, and the last user message can be pulled back into
  the input for editing and resending (F064). Replies render as markdown
  (built-in safe renderer: code fences, lists, links, emphasis) and every
  message has a hover copy button (F067) and a →P button that saves it as a
  prompt template (twin of `prompts add`, F750), and a find-in-conversation
  box highlights matches and counts them (F812). The URL carries the open
  conversation (`/chat?c=ID`), so chats are linkable and reload in place
  (F813). The conversation list has Export
  CSV / Import buttons and a Prune button that deletes chats older than N
  days (pinned kept) — the UI twin of `llmctl chats prune` (F660).
- **Structured output** — give a prompt and a JSON Schema; llmctl asks the
  model for JSON only, validates the reply against the schema and retries
  with the validation error appended until it matches (or gives up with the
  raw reply). REST: POST /api/v1/structured. Dedicated Structured page with
  optional max-tokens/temperature inputs (F359), a validation-retries input
  (1–5, CLI twin `-retries`, F692), a seed input for reproducible replies
  (F795), a top_p nucleus-sampling input (F799) and frequency/presence penalty
  inputs (F805), all persisted in the browser (F765), a "Load schema" button that fills the
  schema box from a .json file (UI twin of `-schema`, F713), a Result Export
  JSON button (F432) with a Copy button for the JSON (F770), a Copy cURL button
  for the request (F786) and a "Save key" button that remembers the API key in the
  browser (F734).
- **Batch inference** — paste prompts (one per line) and run them all
  concurrently through the gateway; per-prompt results and errors in input
  order. REST: POST /api/v1/batch. Dedicated Batch page with optional
  max-tokens/temperature inputs (F359), a concurrency input (1–8 workers,
  CLI twin `-concurrency`, F691), a seed input for reproducible replies
  (F795), a top_p nucleus-sampling input (F799) and frequency/presence penalty
  inputs (F805), all persisted in the browser (F765), a "Load file" button that fills the prompts
  box from a text file (UI twin of `-file`, F712), Results Export CSV/JSON
  buttons (index,prompt,ok,content,error, F424, F732) with a per-row Copy
  button (F770), a Copy cURL button for the request (F786) and a "Save key" button
  that remembers the API key in the browser (F734). CLI:
  `llmctl batch -model ID [-file F|piped stdin|prompts…] [-key K] [-json]
  [-out FILE] [--csv]` (F224; -out writes the raw JSON to a file, F449; --csv
  exports prompt,ok,content,error rows, F605).
- **Usage** — per-model token bar chart (from /api/v1/metrics) and
  per-conversation token table (from /api/v1/chats). Shows prompt/completion
  totals, request counts, per-model latency percentiles (p50/p95/p99), a
  model-id deep link to the Requests page filtered by that model (F742), and the
  4xx/5xx error split, average
  time-to-first-token and completion throughput (tok/s), clickable column
  headers to sort the per-model table (F781), and
  per-conversation context percentage. A "Daily History" table shows the
  per-day, per-model rollup (requests, errors, tokens, estimated cost) which
  persists across restarts (`GET /api/v1/usage/daily[?days=N]`, F097, F103)
  and can be exported as CSV (F117); a days dropdown (7/14/30/90, twin of
  `usage -n`) sets the fetched window (F759); a model text filter narrows the table
  (and its CSV) to models whose id contains the text (F352); a "Trend" card
  charts the last 14 days
  of requests and cost as bar charts (F147). A parallel per-key rollup
  (`GET /api/v1/usage/keys[?days=N][&key=ID]`) attributes daily requests,
  tokens and cost to each API key (F126); the Usage page shows it as a
  "Daily by Key" table with its own CSV/JSON export (F129, F718), and a By tag toggle
  rolls the days up per key tag — the UI twin of `llmctl usage -tag` (F706).
  Rollups are pruned to
   the last `usageRetentionDays` UTC days (Settings → "Usage retention";
   default 90, -1 = keep forever) (F168).
- **Requests** — the last 256 gateway requests (time, model, serving instance,
  key, status, latency, tokens) in an auto-refreshing table with a summary bar
  (count, errors, p50/p95 latency, tokens, cost — the UI twin of
  `llmctl requests -stats`, F258) with its own Export CSV (F421) and Export
  JSON (F718, F729), a groupable
  stats table (by hour/day/status/model/key/instance — the UI twin of
  `llmctl requests -stats -by …`, F549, F554, F594), with one-click
  CSV export, per-row expand to full JSON detail with a Copy JSON button
  (F161, F757), clickable column headers to sort the loaded rows
  (F774), and combinable filters (model, status class or exact code like
  404/429 (F153), min latency,
  stream/plain, key prefix or id (with a key-name dropdown to pick it, F735),
  model and instance dropdown filters (F736),
  and a since/until time range (F189). The model, key, instance, status and
  since/until filters can also be preset from the URL
  (`/requests?model=…&key=…&instance=…&status=…&since=…&until=…`,
  F741, F747, F753), and `?id=…` shows one request by X-Request-Id
  (twin of `requests -id`, F758). The serving replica is also echoed to clients as
  the `X-LLMCtrl-Instance` response header (F115). Authenticated requests also
  echo the serving key's id as the `X-LLMCtrl-Key` response header (F233).
  Each request has an id echoed as the X-Request-Id response header (a
  client-sent X-Request-Id is reused). Keys with a token or request quota get
  X-RateLimit-Limit/-Remaining and X-LLMCtrl-Requests-Limit/-Remaining
  response headers showing what is left (F108); keys with a daily cost budget
  get X-LLMCtrl-Cost-Budget-USD and X-LLMCtrl-Cost-Spent-Today-USD (F693).
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
  has Export CSV and Export JSON buttons on the installed list (F396, F715), and
  a Copy output button under each test-run result (F782).
- **MCP servers** — add Model Context Protocol servers launched as local
  processes (command + args). llmctl performs the MCP handshake, lists each
  server's tools, and offers them to the agent as
  `mcp:<server>:<tool>`. The MCP page supports add / enable / test (shows the
  advertised tools, with Export CSV/JSON on that list, F720) / delete, plus
  Export CSV on the server list (F405); the
  add form carries an Env field (K=V pairs) for the server process environment,
  the UI twin of `mcp add -env` (F656). REST: /api/v1/mcp.
- **Skills** — create reusable prompt templates invoked in chat via
  `/s.<short_name> <text>`. Each skill has a short name, display name,
  description, prompt template (with {{input}} and {{conversation}} placeholders),
  tags, and an enabled flag. Stored as directories at
  `~/.llmctl/skills/<id>/`. The Skills page supports add/edit/remove/enable/disable
  and Export CSV / Export JSON buttons on the installed list (F395, F715),
  per-skill Export JSON
  (manifest, CLI parity) and an Import JSON button on the list (F415), plus a
  per-skill Copy prompt button for the template (F784).
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
  optional max-tokens/temperature inputs (F359), also persisted in the
  browser (F764), a seed input for a fair comparison (F794), a top_p
  nucleus-sampling input (F798) and frequency/presence penalty inputs
  (F804), a persisted system-prompt
  input matching `compare -system` (F668), an attach-file dropdown matching
  `compare -file` (F672), a "Load file" button attaching a local text file as
  context (twin of `-file`, F733) and a Results Export JSON
  button matching `llmctl compare --json` shape (F431) plus an Export CSV
  button matching `llmctl compare --csv` (F607), a per-reply copy button
  in each result panel header (F769) with a per-reply token/latency badge
  (F811), and a Copy cURL button for the first
  selected model (Chat twin, F785). The URL deep-links the selection
  (`/compare?models=a,b&p=...`) so comparisons are shareable (F814). While running, the Compare
  button becomes a Stop button aborting every in-flight stream, keeping
  partial replies (F755).
  Uses the existing /v1/chat/completions SSE endpoint; no new backend API needed.
  A sibling **Bench page** (F671) is the UI twin of `llmctl bench`: pick
  instances, requests and concurrency (persisted in the browser, F766), a seed
  input for reproducible replies (F796), a top_p nucleus-sampling input
  (F800) and frequency/presence penalty inputs (F806), and it measures per-model latency
  percentiles and throughput client-side against the gateway, with Export
  CSV/JSON buttons matching `bench --csv`/`-json` (F714), a Copy cURL button for
  the first selected model (F788), and a Stop button
  that cancels the run mid-flight, keeping finished rows (F756).
  CLI: `llmctl compare -models A,B <prompt>` streams the replies in sequence (F301);
  add `-json` for a machine-readable `[{model, reply|error}]` array (F314),
  or `-out FILE` to write that array to a file (F454).
- **Export/import conversations** — GET /api/v1/chats/{id}/export?format=json|markdown
  downloads a conversation as JSON or Markdown. POST /api/v1/chats accepts a
  conversation JSON body for import. Frontend: Export JSON/MD buttons per
  conversation, a Rename button (F296) that edits the title in place, and an
  Import button in the Conversations header that uploads an export file
  verbatim (tools/system included) under a fresh id and opens it (F369,
  plus "Export CSV"/"Export JSON" buttons there downloading the conversation
  list —
  id,title,model,messages,updated_at, same columns as `chats list --csv`, F392, F727 —
  and a Stats toggle rolling the list up per model (chats, messages, tokens,
  last updated), mirroring `chats -stats` (F633).
  replacing the earlier field-picking section); CLI twin `chats import` (F368).
- **Prompt templates** — curated built-in prompt templates (code review,
  explain, summarize, translate, brainstorm, critique, debate, rewrite) stored
  in internal/prompt/templates.go, plus user-defined custom templates stored
  in state (F306): POST/DELETE /api/v1/prompts, merged into the list with a
  `custom` flag; CLI `prompts add <id> -name N [-content T] [-tags a,b]` / `prompts update <id>
  [-name N …]` / `prompts delete <id>` / `prompts duplicate <id> <new-id> [-name N]`
  (copies any template, built-in or custom, to a new custom id, F638; copies
  the source's tags too, F703);
  custom templates carry free-form tags — `prompts tag <id> [a,b]` (empty clears),
  `prompts -tag T` filters, and the Prompts page shows tag badges, a Tag
  button and a tag-filter dropdown per custom entry (built-ins stay read-only,
  F694, dropdown F695);
  `prompts retag OLD NEW` renames a category across all custom templates in one
  shot (built-ins untouched, F641);
  the Prompts page has a New-template form and per-custom-entry Edit/Delete (F307, F309)
  plus a Duplicate button on every template (built-in or custom) that copies it to a
  new custom id, mirroring `prompts duplicate` (F639), plus a Retag button renaming a
  category across custom templates, mirroring `prompts retag` (F644), plus a live text filter with `?q=` deep links from Search (F322) and an
  Export CSV button on the template list (F394) and a Stats toggle rolling the
  filtered list up per category, mirroring `prompts -stats` (F636);
  `prompts export [-out F]` / `prompts import FILE` move custom templates
  between instances (F323); the Prompts page has matching Export JSON /
  Import JSON buttons (F409).
  REST API: GET /api/v1/prompts,
  GET /api/v1/prompts/categories, GET /api/v1/prompts/{id}. Dedicated Prompts
  page with category filtering, copy-to-clipboard, an "Insert into chat"
  button that opens the Chat page with the template pre-filled (F300), and a
  per-template "Render" preview that fills {{input}} and extra {{k}} vars
  live (F361), with a model picker + Run button that sends the rendered
  template through the gateway and streams the reply inline (UI counterpart
  of `llmctl prompts run`, F596), with a Copy reply button under the reply
  (F773) and a Copy cURL button for the rendered prompt (F789). CLI:
   `llmctl prompts [list] [filter] [-category X] [-json] [-csv] [-stats]` (the `list`
   word is accepted as an explicit subcommand, and `--csv` exports the
   filtered list as id,category,name,description, header-only when empty, F382;
   `-stats` rolls the filtered list up per category — template count, `(none)`
   for uncategorized, F630) and
   `llmctl prompts show <id> [-out file]` and `llmctl prompts render <id> [-input TEXT |
   piped stdin] [-var k=v …] [-out file]` (renders the template with {{input}} and any
   {{k}} placeholders filled, for scripts; -out writes the rendered text to a file, F442)
   and `llmctl prompts categories [-json] [-csv] [-out FILE]` (lists categories, one
   per line; -json prints the raw array, -csv a one-column table, -out writes it to a file, F595, F616)
   (F218, F298, F304).
 - **Chat CLI** — `llmctl chats [filter] [-json] [-out FILE] [-stats]` (or `chats list [filter]`)
   lists saved conversations with a per-chat token total (`--csv` exports the list, F378, F603;
   `-stats` rolls the history up per model — chats, messages, tokens, last updated, F625),
   `llmctl chats show <id> [-json] [-last N] [-grep TEXT] [-full] [-out FILE]` prints one
   with a message preview (and a tags line when the chat is tagged, F678),
   `llmctl chats search <query> [-json] [-csv] [-out FILE]` full-text searches message content
   with match snippets (`--csv` one row per matched message, F617), `chats export <id> [-md] [-out f]` and
   `chats export-all [-out DIR] [-md] [-tag T]` (bulk file export, F325;
   `-tag` exports only chats carrying that tag, F686),
   `chats import <file.json> [-json] [-out FILE]` re-imports an exported chat under a fresh id
   (F368; -out writes the saved chat JSON, F510),
   `chats fork <id> [-at N] [-title T]` (forks inherit the source chat's
   tags, F703), `chats rename <id> <title>`,
   `chats pin|unpin <id>` (pinned chats sort first, F310; the Chat page
   shows a 📌 marker and a Pin/Unpin button, F311),
   `chats tag <id> [a,b]` / `chats retag <old> <new> | retag -rm <tag>` label
   chats with free-form tags (shown in the sidebar and `--csv`, filterable with
   `chats list -tag T`; the Chat page has a Tag button per chat, F676, and a
   tag dropdown filtering the sidebar list, F678),
   `chats delete <id>`, `chats prune -before D [-older-than N] [-yes]`
   (bulk-delete old chats, pinned skipped, dry-run by default, F333), and
   `chats delete-all -yes` manage them
   (over the chat-store API, F244, F267, F269, F295, F299, F310).
 - **Skills CLI** — `llmctl skills [filter] [-json] [-csv] [-tag T]` lists skill packs
   (filter matches id/name/description/tags; `-tag T` keeps only skills carrying
   tag T exactly (F674); `--csv` exports
   id,enabled,name,description,tags with tags joined by `;`, header-only
   when empty, F384); `skills show <id> [-out FILE]|enable|disable|delete <id>`
   manages them, `skills add <manifest.json>` creates one from a JSON
   manifest, `skills update <id> [-name N] [-desc D] [-template T] [-tags a,b]`
   edits one in place (F652), `skills retag <old> <new> | retag -rm <tag>`
   bulk-renames or drops a tag across skills (F663) and `skills export <id>
   [-out f]` dumps one back
   out (F245, F288, F305).
 - **Memory CLI** — `llmctl memory [filter] [-json] [-csv] [-out FILE] [-stats] [-tag T]` lists memory entries
   (`-tag T` keeps only entries carrying tag T exactly, F674;
   `--csv` exports id,created_at,tags (`;`-joined),content, F401; `-stats` rolls entries
   up per tag — entries, last created, with `(none)` for untagged, F626);
   `memory add "<text>" [-tags a,b]`, `memory update <id> [-content C]
   [-tags a,b]`, `memory show <id> [-out FILE]`, `memory search <query> [-json] [-csv] [-out FILE]`,
   `memory delete <id>`; `memory retag OLD NEW` / `memory retag -rm TAG`
   rename or drop a tag across every entry in one shot (F640);
   `memory export [-out F]` / `memory import FILE`
   move entries between instances (F246, F267, F289, F321); the Memory page
   has matching Export JSON / Import JSON buttons (F410), a Duplicate button per
   entry that copies it with editable tags (F642), an Edit button per entry
   (twin of `memory update`, F746), a Retag button that renames or
   removes a tag across all entries mirroring `memory retag` (F643), and a Stats toggle
   that rolls the list up per tag, mirroring `memory -stats` (F632).
 - **KB CLI** — `llmctl kb [filter] [--csv] [-json] [-out FILE] [-tag T]` lists knowledge bases
   (`-tag T` keeps only KBs carrying tag T exactly, F674);
   `kb create <name> [-desc X] [-tags a,b]`, `kb update <id> [-name N] [-desc D]`,
   `kb tag <id> [a,b]` / `kb retag <old> <new> | retag -rm <tag>` label KBs with
   free-form tags — the KB twin of model tags, shown by `kb show`, matched by
   the list filter, exported in `--csv` (new `tags` column) (F664),
   `kb show <id> [-json] [-out FILE]` (human view: description, doc list, search mode —
   embeddings vs TF-IDF — F366; -out writes the raw JSON, F505), `kb docs <id> [-json|-csv|-out FILE]` (F618) | `kb delete <id>`, `kb doc <kb-id> <doc-id> [-out F]` (download a
   document's text, F320) and
   `kb add-doc <kb-id> <path> [-name X]` upload documents,
   `kb export <kb-id> [-out DIR]` dumps all documents as files (F324) and
   `kb search <kb-id> <query> [-limit N] [-json] [-csv] [-out FILE]` runs RAG search,
   `kb search-all <query> [-limit N]` runs one query across every KB and merges
   the top hits by score (each tagged with its knowledge base, F665) and
   `kb embed <kb-id> -url U -model M [-key K] | -clear` configures semantic
   search (F247, F250, F268, F272, F291, F320).
 - **Tools CLI** — `llmctl tools [-json] [-csv] [-out FILE]` lists installed tools
   (`--csv` exports id,enabled,sandbox,runtime,name,description, F389);
   `tools install <dir|https://…/tool.zip> [-id ID]` installs a local pack or a
   packaged .zip over HTTP (custom id supported, F657), `tools show <id> [-out FILE]|enable|disable|delete <id>`
   and `tools run <id> -args '<json>'` (or `-args -` for stdin / `-args-file F`;
   `-out FILE` writes the result to a file, F458)
   execute one from the shell (F248, F250, F338); `tools sandbox <id> <on|off>`
   toggles sandboxed execution for python tools — CLI twin of the Tools page
   toggle (F653).
 - **MCP CLI** — `llmctl mcp [-json] [-csv]` lists configured MCP servers
   (`--csv` exports id,name,enabled,command, F403);
   `mcp add <name> <command> [-args 'a b'] [-env k=v …] [-json] [-out FILE]` (-out writes the raw created-server JSON, F511; `-env` sets the server process's environment, repeatable, and `-json` prints the created record, F655), `mcp remove|enable|disable <id>`,
   `mcp test <id> [--csv|--json] [-out FILE]` (connect and list advertised tools;
   --csv/--json export name,description, F436) and
   `mcp export [-out F]` / `mcp import FILE` (configs move between instances,
   enabled state preserved, F330) (F249); the MCP page has matching
   Export JSON / Import JSON buttons (F411) and an Export CSV on the tested
   server's Tools card with the same columns (F437), plus a Copy JSON button
   for that tools list (F783).
 - **Cost CLI** — `llmctl cost` prints lifetime token/cost totals and a
   per-model table; `-keys` shows per-key costs with names, `-tags` rolls
   them up per key tag (untagged keys under "(untagged)", F705), `-model M` /
   `-key K` filter the table to one id, `--csv` exports the table, `-json` emits
   `{totals, groups}`, `-out FILE` writes that JSON to a file, `-watch N`
   reprints the view every N seconds (F251, F349, F370, F470, F588).
 - **File CLI** — `llmctl files [list|upload <path> [-name X]|cat <id>|delete
   <id>] [--csv] [-json] [-stats]` covers the whole uploaded-files API from the shell, including
   uploads (F219, F243), CSV inventory export (F381), and a `-stats` per-content-type
   roll-up — files, bytes, last upload (F631).
- **File upload** — upload text-based files stored at
  `~/.llmctl/files/<id>/`. REST API: POST /api/v1/files (multipart),
  GET /api/v1/files, GET /api/v1/files/{id}/content, DELETE /api/v1/files/{id},
  PATCH /api/v1/files/{id}/tags (free-form tags, the last entity to join the
  tag family — `files tag <id> [a,b]`, `files list -tag T`, TAGS column in
  list/CSV, tag badges + Tag button + tag-filter dropdown on the Files page,
  F700).
  In chat, `/file <filename>` injects file content as a user message.
  Dedicated Files page for upload/list/delete, with a filename text filter
  (F356), clickable column headers to sort the list (filename/type/size, F778), an Export CSV button (filter-aware, F397), an Export JSON button
  (twin of `files -json`, F716), a per-row Download button saving the stored
  bytes (twin of `files cat -out`, F745), a text-file View preview with a Copy
  button for the content (F761), a Stats toggle rolling the
  list up per content type mirroring `files -stats` (F637), and drag-and-drop upload —
  drop a file onto the upload card to upload it directly (F452). Text files are stored as-is;
  PDF and DOCX uploads have their text extracted automatically (F053).
- **Cost tracking** — track token usage and estimate costs per API key and per
  model. REST API: GET /api/v1/cost (summary), GET /api/v1/cost/keys,
  GET /api/v1/cost/models. Dedicated Cost page with per-key and per-model
  tables showing token counts and estimated USD costs, each with a text
  filter on the id (F353), clickable column headers to sort (F780), id cells deep-linking to the Requests page filtered
  by that key/model (F743), and filter-aware Export CSV / Export JSON buttons
  (F417, F718).
  A By tag toggle rolls the key table up per key tag — the UI twin of
  `llmctl cost -tags`, with untagged keys under "(untagged)" (F705).
- **Scheduled tasks** — cron-scheduled tasks of three kinds: `chat` (send a
  prompt to a model, result saved as a conversation), `instance.start` and
  `instance.stop` (start/stop an instance on schedule — e.g. load a model
  every morning, unload it at night), plus `backup` (state snapshot).
  Chat tasks can name knowledge bases (`-kb`, comma-separated ids in the CLI;
  the Scheduler page dropdown picks one): the top matching chunks are
  injected as context, same as
  `chat -kb`
  (F365, multi-kb F666). Chat tasks can also carry sampling options —
  `-temperature`, `-max-tokens` and `-seed` on `scheduler add` (and matching
  inputs on the Scheduler page) — sent with every scheduled request and kept
  across prompt-dialog edits (F808). Chat tasks can also carry a system
  message (`scheduler add -system`, Scheduler page input) prepended to each
  run (F809). Scheduled chats reach the gateway with a memory-only internal
  token, so they work even when gateway auth is on (F365).
  Tasks can be triggered on demand ("Run now" / `llmctl scheduler run`).
  Tasks carry free-form tags — `llmctl scheduler tag <id> a,b` (empty clears),
  bulk `scheduler retag <old> <new>` / `retag -rm <tag>`, a `scheduler list
  -tag T` filter and TAGS column (table and `--csv`), and a per-row Tag button
  on the Scheduler page; tags survive task updates (F681). The task table has
  clickable column headers to sort (name/cron/kind/model/enabled/next/last, F779).
  `llmctl scheduler add` prints a one-line confirmation with the new task id
  (`-json` for the raw record, F563; `-tags a,b` tags at creation, F702).
  `llmctl scheduler duplicate <id>
  [-name N]` copies an existing task (cron, kind, model, prompt, kb, webhook,
  enabled state, tags all carried over) under a new name — default `<old name> copy`
  (F646); the Scheduler page has a matching Duplicate button on each row (F647).
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
  prompt and PUTs the updated spec, F293) and a "Recent Runs" history of the last
  executions — status, duration and error/result excerpt
  (GET /api/v1/scheduler/runs, F145; the ring keeps the newest 200 and the API
  takes ?limit= and ?task= server-side, F699) with an Export CSV button matching
  `scheduler runs --csv` (F418, F419); `scheduler runs -json` prints the raw
  run log, `scheduler runs -out FILE` writes the
  run-log JSON to a file (F476); `scheduler runs -watch N` live-tails new runs
  every N seconds (F555); `scheduler runs -stats` rolls the log up per task —
  count, ok/errors, average duration, last run (F601), with a matching Stats
  toggle on the Scheduler page (F602) and an Auto-refresh checkbox that polls
  the ring every 5s while on (F608). `scheduler runs -task T` keeps only the
  runs of one task (id or name; the CSV gained a task_id column; the filter and
  `-n` now run server-side via ?task= and ?limit=, F699), and the
  Scheduler page's Recent Runs card has a matching task dropdown (F688) and an
  Export JSON button (twin of `scheduler runs -json`, F728). Each task row also shows its most
   recent run's status (ok/error badge) from that ring, in the Scheduler page
   and `llmctl scheduler list` (F184), plus an "Export CSV" button on the
   task list with the same columns as `scheduler list --csv` (F391).
- **RAG knowledge bases** — create/delete/edit knowledge bases at
  ~/.llmctl/knowledge/<kb_id>/ with kb.json manifest. Add/remove documents
  (text stored as-is; PDF/DOCX text extracted automatically, F053) via REST
  API. In chat, /file <filename> or KB selection injects
  document content as context; several KBs can be selected at once (multi-select
  on the Chat page, comma-separated `-kb A,B` in the CLI) and the merged top
  chunks are injected (F666). Dedicated Knowledge Bases page with an
  Export CSV and Export JSON buttons on the KB list (F393, F717) and per-KB Export/Import JSON
  (all documents as a [{filename, content}] array, the UI counterpart of
  `llmctl kb export`, F435); dropping a file onto the KB detail card uploads
  it as a document (F453). The per-document view modal has Copy and Download
  buttons (Download is the twin of `kb doc -out`, F768). A Tags button per KB (and `llmctl kb tag` /
  `kb retag`) labels knowledge bases with free-form tags, shown as badges
  under the name (F664). A "Search all knowledge bases" box on the KB list
  runs one query across every KB and shows the merged top hits with their
  source KB (F665). Search ranks
  chunks by TF-IDF cosine similarity, or by embedding similarity when the KB
  is pointed at any OpenAI-compatible `/v1/embeddings` endpoint (per-KB
  semantic-search panel in the UI; TF-IDF remains the fallback).
- **API Keys** — generate / list / revoke. Optional per-key token quota
  (total prompt+completion tokens); once spent, the gateway returns 429
  until the quota is raised. Usage counters persist across restarts and are
  tracked per key id, so keys that share a visible prefix never share usage
  (F109). Keys page shows usage and estimated cost, today's per-key usage
  (F127), a live text filter (name/id/prefix, F335), a tag dropdown
  (UI twin of `key list -tag`, F682), clickable column headers to sort the
  list (name/created/used/today, F775), Export CSV / Export JSON buttons for
  the filtered list (same columns as `llmctl key list --csv`, F375, F730), and links
  each key to its
  recent requests (request log filtered by key id, F120); keys can be renamed
  inline (F140) and carry a free-text note edited inline or via
  `llmctl key note <id> <text>` (F186). Keys also carry free-form tags —
  `llmctl key tag <id> a,b` (empty clears), bulk `key retag <old> <new>` /
  `retag -rm <tag>`, a `key list -tag T` filter and TAGS column (table and
  `--csv`), and a per-row tag editor on the Keys page (F680).
  `llmctl key update <id>` edits any
  of those later — name, note, models, quota, caps, rate, expiry, budget (F254).
  `llmctl key duplicate <id> [name]` issues a fresh key with the same
  restrictions (default name `<old name> copy`, F650); the Keys page has a
  matching per-row Duplicate button (F651).
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
   expired" button (F167); `key prune -idle N` additionally removes keys
   unused for N+ days — never-used keys count once created N+ days ago
   (F710), with a matching "Prune idle" button on the Keys page (F711).
   A per-key daily cost budget
   (`dailyCostBudgetUSD`, 0 = unlimited) returns 429 `budget_exceeded` once
   the key's estimated USD spend for the UTC day reaches it (F138). Per-key usage counters (tokens, requests, cost) can
  be zeroed with a "Reset" button, `llmctl key reset-usage <id>` or
  `POST /api/v1/keys/{id}/reset-usage` (F107). Token and request quotas can
   be changed after creation via PATCH and the Keys page quota editor (F148), and
   each key shows when it was last used (F149). `llmctl key show` and
   `GET /api/v1/keys/{id}` also report lifetime totals (requests, errors,
   tokens, cost) summed over the retained per-key daily rollups (F182), and
   `key show` prints a tags line when the key is tagged (F686).
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
  silent), when a key first hits its token quota, request cap or daily
  cost budget, or when an expired key is used — `key.*` events deduped to
  one alert per key+kind per hour, also shown in the events feed
  (F689, expiry F690). A "Test" button sends a test event to verify the endpoint, with
  optional event-name and message-text inputs for realistic payloads (F592,
  message F697; CLI twin: `llmctl webhooks test -event E -message M`, F591). An
  optional signing secret makes every webhook POST (events, test, scheduler
  task results) carry `X-LLMCtrl-Signature: sha256=HMAC(secret, body)` so
  receivers can verify the sender (F111). An "events" filter (comma-separated
  kinds, empty = all) restricts which events are POSTed, e.g. only
  `instance.crashed` (F143). The last 50 delivery attempts (time, event,
  status/error) are listed under the webhook, at
  GET /api/v1/webhook/log (F144), and in `llmctl webhooks` (F191), which
  supports `-n` (server-side `?limit=`) and `-event` (server-side `?event=`)
  filters (F357), `--csv` export (F380), a `-failed` failed-only view (F709)
  and a `-stats` per-event roll-up —
  total/ok/failed/last attempt (F624); the Settings page delivery list has a matching event
  dropdown (F358), a "Failed only" checkbox (F709), filter-aware Export CSV/JSON
  buttons (F416, F748), a per-event Stats roll-up toggle (twin of
  `webhooks -stats`, F749), and an
  Auto-refresh checkbox that polls every 5s while testing a receiver
  (F615). Event and
  scheduler-hook deliveries retry
  up to 3 times (1s, 2s backoff) so a briefly unreachable receiver still gets
  the notification (F163). Each delivery also gets a short id and keeps its
  payload (≤8 KiB), so a failed one can be re-sent with
  `llmctl webhooks retry <id>` / `POST /api/v1/webhook/retry` / a Retry button
  on the failed row in Settings (F679).
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
   or the global `settings.backupKeep`, default 7, F181; `llmctl backups prune
   -keep N` prunes the existing set on demand, F558); the Settings
  page lists those snapshots for download, restore-in-place, integrity
  verification (`llmctl backups verify <name>` / Verify button, F242), download to a file
  (`llmctl backups download <name> [-out f]`, F276), pruning to the newest N
  (`llmctl backups prune` / "Prune old" button, F558, F559) or deletion,
  and
  "Create snapshot now" writes one on demand (`POST /api/v1/backups`; restore via
  `llmctl backups restore <name>` or the Restore button, F175, F176; CLI
  `backups create [-name LABEL]` and a UI label input embed a sanitized label
  in the file name, llmctl-<label>-<ts>.tar.gz, F443, F444), next to an
  Export CSV/JSON buttons matching `backups list --csv`/`-json` (F398, F725).
  `llmctl backups list --csv` exports the snapshot inventory
  (name,size_bytes,created), header-only when empty (F383), and
  `llmctl backups -watch N` reprints the list every N seconds — handy while
  scheduled backups fire (F609); the Settings card has a matching
  Auto-refresh checkbox (F610).
- **API console** — a Settings page card that sends any METHOD + /path (with an
  optional JSON body for POST/PUT/PATCH) straight to the management API and
  pretty-prints the status and response, the browser counterpart of
  `llmctl api` (F589), with a Copy result button for the response (F772) and a
  Copy as cURL button for the request (F787).

Live job progress streams over SSE. Chat state (key, draft, conversation)
survives the periodic UI re-render.

## CLI

```
llmctl up [--addr :8080] [--rate 20] [--burst 60] [--max-inflight 512] [--api-key K]
llmctl down [-json]                      (prints "daemon stopped"; -json prints the raw response, F568)
llmctl status [-json] [--csv] [-watch N] [-out FILE]   (-watch N: live refresh; -json: raw; --csv: running instances as id,model,uptime_sec (F790); -out writes the raw JSON to a file, refreshed each -watch tick, F466; version, uptime, running instances with
                                    uptime, gateway auth, today's usage)
llmctl doctor [-json] [--csv] [-quiet] [-check NAME]... [-watch N] [-out FILE]  (--csv exports name,status,detail (F429); -quiet prints only warn/error lines + a summary; -check keeps matching checks, repeatable; -watch N re-runs the sweep and prints checks as they change, F557; -out writes the JSON to a file, F464)
llmctl gpu [-json] [--csv] [-out FILE] [-watch N]             (GPU inventory: vendor, name, used/total; --csv adds raw bytes + used_pct, -out writes the raw JSON, -watch N reprints every N seconds, F430, F463, F593)
llmctl jobs [-json] [--csv] [-watch N] [-out FILE] [-stats]   (background install jobs; --csv exports the job list, -out writes the raw JSON refreshed each -watch tick, -stats rolls the list up per kind — total/running/done/error/last, F427, F273, F477, F751)
llmctl jobs wait <job-id> [-timeout N] [-interval N] [-json]   (block until the job finishes: exit 0 on done, non-zero on failure; progress prints only when it changes; -json prints the final job JSON; backed by GET /api/v1/jobs/{id}, F575)
llmctl jobs show <job-id> [-json] [-out FILE]   (one job in detail — status, progress, message/error, timestamps, F578; -out writes the raw JSON to a file, F621)
llmctl jobs clear [-json]   (drop finished jobs from daemon memory; running jobs are kept, F585)
llmctl search <query> [-max N] [-json] [--csv] [-out FILE]  (keyless web search; --csv exports rank,title,url,snippet, -out writes the raw JSON, F428, F274, F462)
llmctl structured -model ID -schema f.json [-key K] [-max-tokens N] [-temperature T] [-seed N] [-top-p P] [-freq-penalty F] [-pres-penalty P] [-out FILE] <prompt>   (schema-validated JSON, F279, F344; -out writes it to a file, F457; -seed for reproducible replies, F795)
llmctl embed -model ID [-key K] [-json] [-out FILE] <text>     (gateway embeddings, F281; -out writes the JSON to a file, F456)
llmctl complete -model ID [-key K] [-json] [-seed N] [-top-p P] [-freq-penalty F] [-pres-penalty P] [-out FILE] <prompt>  (gateway text completions, F287; -out writes the text to a file, F455; -seed/-top-p sampling, F801)
llmctl compare -models A,B [-system S] [-max-tokens N] [-temperature T] [-file PATH] [-json] [--csv] [-out FILE] <prompt> (CLI twin of Compare page, F301, F314, F343; -out writes the JSON array to a file, F454; --csv one row per model, F606; -file prepends a local file as context to every model, F670)
llmctl count <text> [-file F] [-chat ID] [-model ID] [-json] [-out FILE]  (estimate tokens, ~4 chars/token; -chat sizes a saved conversation, F317, F336; -out writes the counts JSON to a file, F461)
llmctl bench -model ID [-key K] [-n N] [-conc C] [-seed N] [-top-p P] [-freq-penalty F] [-pres-penalty P] [-json|--csv] [-out FILE]  (latency/throughput benchmark, F282; single-model also supports the JSON/CSV/-out shapes, F623; -seed for reproducible replies, F796)
llmctl bench -models A,B [-n N] [-conc C] [-json|--csv] [-out FILE]  (multi-model comparison table; --csv one row per model, -out writes the stats JSON to a file, F426, F303, F315, F460)
llmctl disk [-json] [--csv] [-out FILE] [-watch N]            (model dir + volume disk usage; --csv one row, raw bytes, -out writes the raw JSON, -watch N reprints every N seconds, F451, F465, F590)
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
                                    references a missing model or recent runs failed, F334), embeddings
                                    (warns when a KB's configured embedding endpoint is unreachable, F552);
                                    exit ≠0 on errors)
llmctl ps [-watch N] [--csv] [-json] [-out FILE] [-stats] [-tag T]   (-watch N: live refresh; --csv: one-shot CSV export; -json: raw, printed each -watch tick; -out writes the raw JSON to a file, refreshed each -watch tick, F467, F620; -stats: per-model roll-up — replicas, running, served, restarts, F629; -tag T: only instances carrying tag T, applied to every output mode, F675; also shows uptime, requests served, auto-restart
                                    count, per-instance notes and tags)
llmctl backends list|install|uninstall|add|remove   (`list --csv` exports id,transport,kind,installed,version,description, F388; mutations print one-line confirmations, `-json` for raw, F566; install `--wait` blocks until the job finishes, F576)
llmctl catalog list [-json|-csv] | install <id> [--wait]      (curated model picker, F266; `list --csv` exports id,name,backend,size_gb,quant,tags, F387; install `--wait` blocks until the download finishes, F576; the Models page "Featured models" table sorts by name/quant/size, F777)
llmctl presets list [-json|-csv]|save|duplicate|start|delete|export|import  (named start recipes, F265, F328; save/start -json print the raw response, F524, F525; duplicate <name> <new-name> copies model/instance/backend/port/vars, F648)
llmctl models list|show|install|remove|hf-search|hf-files   (list shows human sizes + total, --csv for export, -json for the raw array, -out FILE writes the raw JSON, F479; install `--wait` blocks until the download finishes, F576,
llmctl models add <id> <file> [-backend ID] [-ctx N] [-tags a,b] [-json] [-out FILE] (register an existing local GGUF, F275; -ctx sets the default context window and -json prints the raw created-model JSON, F551; -out writes it to a file, F513; -tags tags at creation, F702)
llmctl models rename <id> <new-id> [-json]           (change a model id; aliases follow, F284; -json prints the raw response, F523)
llmctl models note <id> [text…] [-json]              (free-text annotation on a model; empty text clears, F661)
llmctl models tag <id> [a,b,c] [-json]               (replace a model's tags; empty clears, F662)
llmctl models retag <old> <new> | retag -rm <tag>    (bulk rename/drop a tag across models, F662; list -tag X filters)
                                    price and idle-unload; show also lists aliases pointing at the
                                    model (F367); list/show/aliases/hf-search/hf-files --json for scripts;
                                    remove --file also deletes a registered external file)
llmctl models aliases|alias <name> <id>|unalias <name>   (gateway model aliases, F180; `aliases --csv` exports alias,model, F385; `aliases -json` prints the raw array, `aliases -out FILE` writes the raw JSON, F498; alias/unalias accept -json, F519)
llmctl models probe [-json] [-out FILE]              (what /v1/models serves now, F252; -out writes the raw JSON, F501)
llmctl models routable [-key K] [-json|-csv|-out FILE]  (what /v1/models serves for one key, via the gateway — reflects the key's allowlist, F337; --csv exports id,owned_by, F407; -out writes the raw JSON, F499)
llmctl models ctx <id> <tokens> | price <id> <in/M> <out/M> | idle <id> <min|never> [-json] [-out FILE]   (model settings, F255; human summary by default, -json prints the raw response and -out writes it to a file, F520–F522, F560)
llmctl models metadata <id> [-json] [-out FILE]        (GGUF header metadata, F268; -out writes the raw JSON, F500)
llmctl models unused [--csv] [-out FILE]              (models with no instance + reclaimable size, F280; --csv exports id,size_bytes, F408; -out writes the same as JSON, F512)
llmctl models stats [-json] [--csv] [-out FILE]      (per-backend roll-up — models, running, total size, F627)
llmctl start <model> [--backend id] [--instance name] [--var k=v ...] [-json]
llmctl stop <instance> [--force] [--json]
llmctl stop --all                          (stop every running instance, attached ones stay, F278; human confirmation by default, --json for raw, F566)
llmctl restart <instance> [-json] | llmctl restart --all [--json]   (single restart prints "restarted X", --all reports per-instance failures, F445; --json prints the raw response or, with --all, one aggregate {"restarted":[…]} object, F533, F567)
llmctl maxparallel <instance> <n> [-json]  (concurrent-request cap, 0 = unlimited, F270; -json prints the raw response, F532)
llmctl note <instance> [text] [-json]        (free-text annotation, F271; empty text clears the note, F645; -json prints the raw response, F529)
llmctl tag <instance> [a,b,c] [-json] | retag <old> <new> | retag -rm <tag>   (instance tags, empty clears; retag bulk-renames/drops across instances, F673)
llmctl autorestart <instance> <on|off> [-json]  (crash auto-restart toggle, F271; -json prints the raw response, F530)
llmctl setvars <instance> [-var k=v…] [-json]  (replace stored start vars, F271; -json prints the raw response, F531)
llmctl logs <instance> [--stream stderr|stdout] [--tail N] [-out FILE] [-f]
                                    (-out FILE: download the full log file, F292)
llmctl logs -daemon [--tail N] [-json] [-out FILE] [-f]  (daemon's own log, ~/.llmctl/llmctl.log; -f follows it, F355; -out writes the tail to a file, F516; -json prints the raw {size,logs}, F517)
llmctl key generate|duplicate|show|note|tag|retag|update|enable|disable|rotate|list|revoke|prune|reset-usage   (generate: -quota -maxreq -models -expires -budget -rps -note -tags; tag <id> [a,b] sets free-form tags (empty clears) and retag <old> <new> / retag -rm <tag> renames or drops one across all keys (F680); generate -tags a,b tags at creation, mirrored by a tags input in the Keys page create form (F701); list -tag T filters by tag; duplicate <id> [name] creates a fresh key with the same restrictions — quota, models, caps, rate, budget, note, tags, expiry rounded up to whole days (F650, tags F702); generate -out FILE writes the creation JSON (incl. the secret) to a 0600 file, F507; rotate -out FILE likewise for the new secret, F508; list -out FILE writes the raw JSON, F481; show -out FILE writes the raw key JSON, F506; prune -out FILE writes the result JSON, F509; note/enable/disable -json print the raw response, F528, F535; revoke prints a one-line confirmation, -json for raw, F565; rotate prints the new secret with a save-it-now reminder and reset-usage confirms the reset, -json for raw, F572;
                                        list --csv exports the audit table (F374);
                                        generate/rotate -json prints the raw JSON incl. the secret;
                                        show: one key's full view (-json for scripts); prune: -dry-run lists; note: set a free-text note;
                                        list shows usage, cost,
                                        quota, expiry, daily budget, note (--json for scripts); prune deletes expired keys)
llmctl scheduler list|show|add|duplicate|update|run|runs|enable|disable|delete   (list/runs --json for scripts; list --csv for audit export (F376); list -out FILE writes the raw JSON, F495; show -out FILE writes the raw task JSON, F504; duplicate copies a task under a new name, default "<old> copy" (F646); delete prints a confirmation, -json for raw, F571;
                                    add: -name -cron -kind -model -prompt -instance -webhook -keep -kb;
                                    update: same flags, changes only what you pass, F290)
llmctl requests [--model ID] [--status 4|404] [--min-ms N] [-n 20] [--csv] [--json] [-since 30m|RFC3339] [-until …] [-watch N] [-stats [-by model|key|instance|day|hour|status]] [-out FILE]   (--csv also exports -stats rows, F420; -out writes the raw JSON, also with -id, F473)
llmctl backups [list|create [-name LABEL]|restore <name>|prune [-keep N]|verify <name|-all>|delete <name]]   (list: human sizes + total, --json, -out FILE writes the list JSON, F475; prune deletes all but the newest N snapshots, F558; verify -all checks every snapshot, F561; create/restore/delete print one-line confirmations, `-json` for raw, F564)
llmctl usage [-n 14] [-key ID | -keys | -tag T] [-model ID] [--csv] [--json] [-out FILE] [-watch N]   (-out writes the usage JSON — raw, or the -model-filtered one — to a file, F474; -watch N reprints the view every N seconds, F587; -tag T rolls up the days across every key carrying the tag, F704)
llmctl metrics [-watch N] [-model M] [--csv] [-json] [-out FILE]  (-watch N: live refresh every N seconds; -model M: filter the table; --csv: one-shot CSV export; -json printed each -watch tick; -out writes the raw JSON to a file, refreshed each -watch tick, F469, F620)
llmctl events [-n 20] [-watch N] [-kind K] [-model M] [-key K] [-json] [-out FILE] [-stats]   (-out writes the raw JSON to a file, F468; -stats aggregates one row per kind — count, last seen, instances/models — with --csv and -json/-out of the rollup, F599; -key keeps only key.* alerts for that key name, F696)
llmctl webhooks [-n 20] [-event E] [--csv] [-json] [-watch N] [-out FILE] [-stats] | webhooks test [-event E] [-message M] [-json] | webhooks retry <id> [-json]    recent delivery attempts / send a test event / re-send a logged delivery by id (F447; -out writes the delivery-log JSON, F478; -watch N live-tails new deliveries, F556; -stats rolls the log up per event — total/ok/failed/last, F624; test prints a delivery confirmation, -json for raw, F573; test -event/-message send a custom payload, F591; retry re-posts the stored payload of a delivery, F679)
llmctl prompts [filter] [-category X] [-json] [-csv] [-out FILE] [-stats] | prompts show <id> [-out file] | prompts render <id> [-input TEXT] [-var k=v…] [-out file] | prompts run <id> -model M [-input TEXT] [-var k=v…] [-system S] [-temperature T] [-max-tokens N] [-seed N] [-top-p P] [-freq-penalty F] [-pres-penalty P] [-save TITLE | -chat ID] [-json] [-out FILE]   (run renders the template and sends it to the gateway, F579; -save keeps the exchange as a saved chat, F583; -chat continues one, F584; -out writes the reply text — or the raw JSON with -json — to a file, F614; -stats counts templates per category, F630; -seed/-top-p sampling, F802)
llmctl files [list|upload <path>|cat <id> [-out FILE]|tag <id> [a,b]|delete <id>] [--csv] [-json] [-out FILE on list, F485; cat -out writes the stored bytes to a file, F613; -stats on list rolls up per content type, F631; -tag T on list filters by tag, F700]
llmctl skills|memory|kb|tools delete <id> [-json] | mcp remove <id> [-json]   (delete/remove print the raw response with -json, F536)
llmctl skills|tools|mcp enable|disable <id> [-json]   (toggles print the raw response with -json, F537)
llmctl presets delete <name> [-json] | chats delete <id> [-json] | prompts delete <id> [-json] | memory add "<text>" [-json] | kb create <name> [-json]   (print the raw response/created-record JSON with -json, F538)
llmctl kb add-doc <kb> <path> [-json] | kb update <id> … [-json] | memory update <id> … [-json] | prompts update <id> … [-json] | key update <id> … [-json] | chats pin|unpin <id> [-json]   (print the raw response JSON with -json, F539)
llmctl kb tag <id> [a,b,c] [-json] | kb retag <old> <new> | kb retag -rm <tag> [-json]   (free-form tags on knowledge bases; retag bulk-renames or drops, F664)
llmctl kb search-all <query> [-limit N] [-json|-csv|-out FILE]   (one query across all KBs, merged by score, F665)
llmctl scheduler update <id> … [-json] | tools install <dir> [-json] | backups verify <name> [-json] | skills add <manifest.json> [-json]   (print the raw response JSON with -json, F540; scheduler update also takes -system/-temperature/-max-tokens/-seed, F810)
llmctl catalog install <id> [-json] | kb embed <kb> … [-json]   (print the raw response JSON with -json, F541)
llmctl presets|scheduler|memory|mcp|prompts|autostart import <file.json> [-json]   (print {"created","skipped"} as JSON; per-item skip notices move to stderr, F542)
llmctl chats fork|rename|prune … [-json] | prompts add <id> -name N … [-json] | prompts duplicate <id> <new-id> … [-json]   (print the raw response / result JSON with -json, F544, F638)
llmctl backup [file] [-json] | backup verify <file> [-json] | restore <file> [-json]   (print {file,bytes} / {file,entries,ok} / {restored,home} as JSON, F545)
llmctl backups restore|download|delete … [-json] | chats delete-all -yes [-json]   (raw response / {file,bytes} / {deleted,total} with -json, F546)
llmctl api <GET|POST|PUT|PATCH|DELETE> /path [-d JSON | -d-file F|-] [-out FILE]   raw management-API call; body printed verbatim, non-2xx exits non-zero (F547)
llmctl open [route] [-print]   open the web UI (or one page, e.g. `llmctl open requests`) in the default browser (xdg-open / open / rundll32); -print just shows the URL (F577, F598)
llmctl requests -stats -by day   per-UTC-day aggregate rows added to the model/key/instance groupings (F548); -by status groups per status code for error triage (F553); -by hour buckets by UTC hour for same-day analysis (F582)
llmctl settings [-json] [-out FILE] | settings set -require-auth -idle-min -notify-webhook   (set prints the changed flags, `-json` prints the raw updated settings, F581)
                 -notify-secret -allow-cors -start-on-demand -max-body-mb
                 -rate-rps -rate-burst -retention-days -log-level -backup-keep
                 | settings test-webhook [-event E] [-message M] [-json] (send a test event, optionally with a custom event name/message; prints a delivery confirmation, F574, F591)
llmctl batch -model ID [-system S] [-key K] [-concurrency N] [-temperature T] [-seed N] [-top-p P] [-freq-penalty F] [-pres-penalty P] [-file F|stdin|prompts…] [-json] [--csv] [-out FILE]
llmctl chat [--model ID] [--key KEY] [--system TEXT]
                                    (-prompt TEXT or piped stdin: one-shot mode — send one message,
                                     print the streamed reply, exit; for scripts, F277;
                                     add -save "Title" to also store the exchange as a saved chat, F294,
                                     with -tags a,b to tag it at creation (F687);
                                     add -chat ID to send the saved chat's history and append the reply to it, F297,
                                     in the REPL too — the history loads and every exchange is appended back, F685;
                                     continuations keep the chat's tags (F687);
                                     add -continue to append to the most recently updated saved chat, F312, REPL too F685;
                                     add -json to print {model, prompt, reply, chat_id} as one JSON object, F313;
                                     add -temperature/-max-tokens sampling options, F319;
                                     add -seed N/-top-p P sampling options (reproducible + nucleus sampling, F801);
                                     add -freq-penalty F/-pres-penalty P sampling options (F807);
                                     add -kb ID[,ID2…] to prepend the top knowledge-base matches as context, F302, comma-separated ids merge across KBs F666, applies in the REPL too F667;
                                     add -file PATH to prepend a local file's contents as context (like the Chat UI /file, one-shot and REPL, F669);
                                     add -out FILE to write the reply text to a file instead of stdout, F448)
                                    (-agent with -prompt: tool-using agent loop, prints [tool …]
                                     lines and step count, F285)
                                    (interactive REPL: /help /clear /system TEXT /model ID
                                     /kb ID[,ID…] (/kb - detaches) /save [title] /quit, F331, F342, multi F667;
                                     /title TEXT and /tag [a,b|-] rename / tag the chat saved by /save, F683;
                                     /regenerate drops the last reply and re-sends the last message, F684)
llmctl version [--json] [-out FILE]            (--json prints {"cli","daemon"}; daemon empty when not running, F440; -out writes that JSON to a file, F518)
llmctl completion [bash|zsh|fish|powershell]         (print a shell-completion script for llmctl + subcommands, F341, F350, F354; powershell adds a Register-ArgumentCompleter for $PROFILE, F425; bash/zsh/fish also complete entity ids — models, instances, keys, kb/memory/prompt/chat/skill/tool/mcp/scheduler/backup ids — from the running daemon, F550)
```

## Design principles
- Simple over clever: stdlib Go, one binary, one port, one state file.
- Everything the UI does is an API call (agents/CI can use the same surface).
- Pluggable by data (JSON), not by code, for common cases.
- Prefer stdio IPC for backends that support it; HTTP otherwise.

## Not yet (see docs/PLAN.md)
Docker image, per-key quotas, curated catalog.
