// Typed client for the llmctl management API (/api/v1) and gateway (/v1).
// All UI features are API-backed, so this is the single data path.

export interface Status {
  ok: boolean;
  version: string;
  gateway: string;
  backends: number;
  models: number;
  instances: number;
}

export interface Backend {
  id: string;
  name: string;
  description: string;
  transport: "http" | "stdio";
  kind: "process" | "attach";
  installed: boolean;
  version?: string;
  plugin?: boolean;
}

export interface Model {
  id: string;
  backendId: string;
  path: string;
  sizeBytes: number;
  downloaded: boolean;
  running?: boolean;
  instances?: string[];
  ctxSize?: number;
  idleUnloadMin?: number;
  priceInPerM?: number;
  priceOutPerM?: number;
  note?: string;
  tags?: string[];
}

export interface Instance {
  id: string;
  modelId: string;
  backendId: string;
  transport: "http" | "stdio";
  port: number;
  status: string;
  attached: boolean;
  pid: number;
  startedAt: string;
  lastUsed?: string;
  inflight?: number;
  served?: number;
  restartCount?: number;
  maxParallel?: number;
  note?: string;
  tags?: string[];
  vars?: Record<string, string>;
  lastError?: string;
  autoRestart?: boolean;
}

export interface InstanceLogTail {
  instance: string;
  stream: string;
  size: number;
  truncated: boolean;
  logs: string;
}

export interface CatalogEntry {
  id: string;
  name: string;
  description: string;
  repo: string;
  file: string;
  backend: string;
  sizeGB: string;
  quant: string;
  tags?: string[];
}

export interface AutoStartSpec {
  name: string;
  modelId: string;
  backendId?: string;
  port?: number;
  vars?: Record<string, string>;
}

export interface PresetSpec {
  name: string;
  instance?: string;
  modelId: string;
  backendId?: string;
  port?: number;
  vars?: Record<string, string>;
}

export interface ToolParam {
  type?: string;
  description?: string;
  required?: boolean;
}

export interface ToolRecord {
  id: string;
  dir: string;
  manifest: {
    name: string;
    description?: string;
    entry: string;
    runtime?: string;
    sandbox?: boolean;
    params?: Record<string, ToolParam>;
  };
  enabled: boolean;
  sandbox: boolean;
  network?: boolean;
  source: string;
  installedAt: string;
}

export interface ToolRunResult {
  ok: boolean;
  result?: string;
  stderr?: string;
  exitCode: number;
  error?: string;
  sandbox: boolean;
  backend?: string;
  duration: string;
}

export interface SkillRecord {
  id: string;
  dir: string;
  manifest: {
    id: string;
    short_name: string;
    name: string;
    description?: string;
    prompt_template: string;
    tags?: string[];
    enabled: boolean;
    created_at: string;
    updated_at: string;
  };
  enabled: boolean;
  source: string;
  created_at: string;
  updated_at: string;
}

export interface MemoryEntry {
  id: string;
  dir: string;
  entry: {
    id: string;
    content: string;
    tags?: string[];
    source?: string;
    created_at: string;
    updated_at: string;
  };
  source: string;
}

export interface CostCounter {
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  requests: number;
  cost_usd: number;
  updated_at: string;
}

export interface CostSummary {
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cost_usd: number;
}

export interface ScheduleSpec {
  id: string;
  name: string;
  cron_expr: string;
  kind?: string; // "chat" (default) | "instance.start" | "instance.stop"
  instance?: string;
  model: string;
  prompt: string;
  system?: string; // optional system message for chat runs (F809)
  kb?: string; // knowledge base id: inject top chunks as context (chat kind, F365)
  temperature?: number; // optional sampling temperature (chat kind, F808)
  max_tokens?: number; // optional max tokens per reply (chat kind, F808)
  seed?: number; // optional sampling seed (chat kind, F808)
  keep?: number; // backup kind: snapshots to retain
  enabled: boolean;
  webhook_url?: string;
  tags?: string[]; // free-form labels (F681)
  last_run: string;
  next_run: string;
  created_at: string;
  updated_at: string;
  lastRun?: ScheduleRun; // most recent run from the in-memory ring (F184)
}

export interface ScheduleRun {
  time: string;
  taskId: string;
  name: string;
  kind: string;
  status: string; // "ok" | "error"
  durationMs: number;
  detail?: string;
}

export interface FileEntry {
  id: string;
  dir: string;
  entry: {
    id: string;
    filename: string;
    contentType: string;
    size: number;
    source: string;
    tags?: string[]; // free-form labels (F700)
    created_at: string;
  };
  source: string;
}

export interface ModelMetadata {
  id: string;
  path: string;
  version: number;
  nTensors: number;
  summary: {
    architecture?: string;
    name?: string;
    quant?: string;
    contextLength?: number;
    blockCount?: number;
    tokenizer?: string;
  };
  metadata: Record<string, unknown>;
}

export interface KBEmbedConfig {
  url: string;
  model: string;
  apiKey?: string;
}

export interface KB {
  id: string;
  dir: string;
  kb: {
    id: string;
    name: string;
    description?: string;
    tags?: string[];
    created_at: string;
    updated_at: string;
    doc_count: number;
    embed?: KBEmbedConfig;
  };
  docs: KBDoc[];
}

export interface KBDoc {
  id: string;
  filename: string;
  content_type: string;
  size: number;
  source: string;
  created_at: string;
  content?: string;
}

export interface KBDocContent {
  content: string;
}

export interface KBSearchResult {
  doc_id: string;
  filename: string;
  chunk_text: string;
  score: number;
  chunk_index: number;
}

// Cross-KB search hit: a KBSearchResult tagged with its knowledge base (F665).
export interface KBAllSearchResult extends KBSearchResult {
  kb_id: string;
  kb_name: string;
}

export interface PromptTemplate {
  id: string;
  name: string;
  description: string;
  category: string;
  content: string;
  custom?: boolean;
  tags?: string[]; // free-form labels on custom prompts (F694)
}

export interface ChatMessage {
  role: "user" | "assistant";
  content: string;
  tools?: AgentToolEvent[];
  tokens?: {
    prompt?: number;
    completion?: number;
    total?: number;
  };
}

export interface ChatConversation {
  id: string;
  title: string;
  model?: string;
  agentMode?: boolean;
  system?: string;
  tags?: string[]; // free-form labels (F676)
  messages: ChatMessage[];
  createdAt: string;
  updatedAt: string;
  context?: {
    usedTokens: number;
    limitTokens: number;
    pct: number;
  };
}

export interface ChatSummary {
  id: string;
  title: string;
  model?: string;
  agentMode?: boolean;
  pinned?: boolean;
  tags?: string[]; // free-form labels (F676)
  messages: number;
  tokens?: number; // total token usage (F603, F604)
  createdAt: string;
  updatedAt: string;
  context?: {
    usedTokens: number;
    limitTokens: number;
    pct: number;
  };
}

export interface SearchMatch {
  role: string;
  content: string;
  index: number;
}

export interface SearchChatResult {
  conversationId: string;
  title: string;
  model?: string;
  updatedAt: string;
  matches: SearchMatch[];
}

export interface GatewayMetrics {
  requests_total: number;
  inflight: number;
  tokens: { prompt: number; completion: number; total: number };
  latency: { p50_ms?: number; p90_ms?: number; p99_ms?: number; samples?: number };
  status_classes: Record<string, number>;
  models: Record<string, { requests: number; errors: number; prompt_tokens: number; completion_tokens: number; latency_p50_ms?: number; latency_p95_ms?: number; latency_p99_ms?: number }>;
}

export interface GPUInfo {
  vendor: string;
  name: string;
  total_bytes: number;
  used_bytes: number;
}

export interface MCPServer {
  id: string;
  name: string;
  command: string;
  args?: string[];
  env?: Record<string, string>;
  enabled: boolean;
}

export interface MCPToolDef {
  name: string;
  description?: string;
  inputSchema?: Record<string, unknown>;
}

export interface ModelAlias {
  alias: string;
  model: string;
}

export interface BatchResult {
  prompt: string;
  content?: string;
  error?: string;
}

export interface RequestLogEntry {
  id?: string;
  time: string;
  model?: string;
  key?: string;
  keyId?: string;
  instance?: string;
  status: number;
  latencyMs: number;
  ttftMs?: number;
  promptTok: number;
  compTok: number;
  cost?: number;
  error?: string;
  stream?: boolean;
}

export interface Job {
  id: string;
  kind: string;
  target: string;
  status: "running" | "done" | "error";
  progress: number;
  message?: string;
  error?: string;
  created: string;
  updated?: string;
}

export interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  createdAt: string;
  tokenQuota?: number;
  usedTokens: number;
  usedCost?: number;
  usedRequests?: number;
  maxRequests?: number;
  allowedModels?: string[];
  maxConcurrent?: number;
  rateRPS?: number;
  rateBurst?: number;
  expiresAt?: string;
  dailyCostBudgetUSD?: number;
  todayCost?: number;
  lastUsed?: string;
  lifetimeRequests?: number;
  lifetimeErrors?: number;
  lifetimePromptTokens?: number;
  lifetimeCompletionTokens?: number;
  lifetimeCost?: number;
  note?: string;
  tags?: string[]; // free-form labels (F680)
  disabled?: boolean;
}

export interface WebhookDelivery {
  id?: string; // delivery id for retry (F679)
  time: string;
  url: string;
  event?: string;
  status?: number;
  ok: boolean;
  error?: string;
}

export interface Settings {  requireAuth: boolean;
  idleUnloadMin: number;
  notifyWebhook?: string;
  notifyWebhookSecret?: string;
  notifyEvents?: string[];
  allowCORS?: boolean;
  startupTimeoutSec?: number;
  maxBodyMB?: number;
  startOnDemand?: boolean;
  rateRPS?: number;
  rateBurst?: number;
  usageRetentionDays?: number;
  logLevel?: string;
  backupKeep?: number;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function req<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const r = await fetch("/api/v1" + path, {
    headers: { "Content-Type": "application/json" },
    ...opts,
  });
  const text = await r.text();
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch {
    data = text;
  }
  if (!r.ok) {
    const msg =
      typeof data === "object" && data !== null && "error" in data
        ? String((data as { error: unknown }).error)
        : typeof data === "string"
          ? data
          : `HTTP ${r.status}`;
    throw new ApiError(r.status, msg);
  }
  return data as T;
}

export const api = {
  status: () => req<Status>("/status"),
  shutdown: () => req<{ ok: boolean; msg: string }>("/shutdown", { method: "POST" }),
  backends: () => req<Backend[]>("/backends"),
  installBackend: (id: string) =>
    req<{ job: string; status: string }>(`/backends/${encodeURIComponent(id)}/install`, { method: "POST" }),
  uninstallBackend: (id: string) =>
    req<{ ok: boolean }>(`/backends/${encodeURIComponent(id)}/uninstall`, { method: "POST" }),
  addPlugin: (plugin: object) => req<{ ok: boolean }>("/backends", { method: "POST", body: JSON.stringify(plugin) }),
  removePlugin: (id: string) => req<{ ok: boolean }>(`/backends/${encodeURIComponent(id)}`, { method: "DELETE" }),
  models: () => req<Model[]>("/models"),
  registerModel: (body: { id: string; path: string; backend?: string; ctxSize?: number; tags?: string[] }) =>
    req<Model>(`/models`, { method: "POST", body: JSON.stringify(body) }),
  installModel: (body: { repo: string; file?: string; revision?: string; backend?: string; id?: string }) =>
    req<{ job: string; status: string; model: string }>("/models/install", { method: "POST", body: JSON.stringify(body) }),
  removeModel: (id: string) => req<{ ok: boolean }>(`/models/${encodeURIComponent(id)}`, { method: "DELETE" }),
  setModelCtx: (id: string, ctxSize: number) =>
    req<{ ok: boolean; restartRequired?: boolean }>(`/models/${encodeURIComponent(id)}/ctx`, { method: "PUT", body: JSON.stringify({ ctxSize }) }),
  setModelIdle: (id: string, idleUnloadMin: number) =>
    req<{ ok: boolean; idleUnloadMin: number }>(`/models/${encodeURIComponent(id)}/idle`, { method: "PATCH", body: JSON.stringify({ idleUnloadMin }) }),
  setModelPrice: (id: string, priceInPerM: number, priceOutPerM: number) =>
    req<{ ok: boolean; priceInPerM: number; priceOutPerM: number }>(`/models/${encodeURIComponent(id)}/price`, { method: "PATCH", body: JSON.stringify({ priceInPerM, priceOutPerM }) }),
  renameModel: (id: string, newId: string) =>
    req<{ ok: boolean; id: string }>(`/models/${encodeURIComponent(id)}/rename`, { method: "PATCH", body: JSON.stringify({ newId }) }),
  setModelNote: (id: string, note: string) =>
    req<{ ok: boolean }>(`/models/${encodeURIComponent(id)}/note`, { method: "PATCH", body: JSON.stringify({ note }) }),
  setModelTags: (id: string, tags: string[]) =>
    req<{ ok: boolean; tags: string[] }>(`/models/${encodeURIComponent(id)}/tags`, { method: "PATCH", body: JSON.stringify({ tags }) }),
  modelMetadata: (id: string) =>
    req<ModelMetadata>(`/models/${encodeURIComponent(id)}/metadata`),
  hfSearch: (q: string, limit = 15) =>
    req<Array<{ modelId?: string; id?: string; downloads?: number }>>(`/hf/search?q=${encodeURIComponent(q)}&limit=${limit}`),
  hfFiles: (repo: string, revision = "main") =>
    req<Array<{ Path: string; SizeBytes: number; OID: string }>>(`/hf/files?repo=${encodeURIComponent(repo)}&revision=${encodeURIComponent(revision)}`),
  instances: () => req<Instance[]>("/instances"),
  startInstance: (body: { model: string; backend?: string; instance?: string; port?: number; vars?: Record<string, string> }) =>
    req<Instance>("/instances", { method: "POST", body: JSON.stringify(body) }),
  stopInstance: (id: string) => req<{ ok: boolean }>(`/instances/${encodeURIComponent(id)}`, { method: "DELETE" }),
  stopAll: () => req<{ ok: boolean; stopped: string[] }>("/instances/stop-all", { method: "POST" }),
  restartInstance: (id: string) =>
    req<Instance>(`/instances/${encodeURIComponent(id)}/restart`, { method: "POST" }),
  setAutoRestart: (id: string, enabled: boolean) =>
    req<{ ok: boolean; autoRestart: boolean }>(`/instances/${encodeURIComponent(id)}/autorestart`, {
      method: "POST",
      body: JSON.stringify({ enabled }),
    }),
  setInstanceVars: (id: string, vars: Record<string, string>) =>
    req<{ ok: boolean; vars: Record<string, string>; restartRequired: boolean }>(
      `/instances/${encodeURIComponent(id)}/vars`,
      { method: "PUT", body: JSON.stringify({ vars }) }
    ),
  setInstanceCap: (id: string, maxParallel: number) =>
    req<{ ok: boolean; maxParallel: number }>(
      `/instances/${encodeURIComponent(id)}/maxparallel`,
      { method: "PATCH", body: JSON.stringify({ maxParallel }) }
    ),
  setInstanceNote: (id: string, note: string) =>
    req<{ ok: boolean }>(`/instances/${encodeURIComponent(id)}/note`, {
      method: "PATCH",
      body: JSON.stringify({ note }),
    }),
  setInstanceTags: (id: string, tags: string[]) =>
    req<{ ok: boolean; tags: string[] }>(`/instances/${encodeURIComponent(id)}/tags`, {
      method: "PATCH",
      body: JSON.stringify({ tags }),
    }),
  instanceLogs: (id: string, stream: "stdout" | "stderr" = "stderr", bytes = 65536) =>
    req<InstanceLogTail>(`/instances/${encodeURIComponent(id)}/logs?stream=${stream}&bytes=${bytes}`),
  metrics: () => req<GatewayMetrics>("/metrics"),
  dailyUsage: (days = 14) =>
    req<Record<string, Record<string, { requests: number; errors: number; prompt: number; comp: number; cost?: number }>>>(
      `/usage/daily?days=${days}`
    ),
  dailyKeyUsage: (days = 14) =>
    req<Record<string, Record<string, { requests: number; errors: number; prompt: number; comp: number; cost?: number }>>>(
      `/usage/keys?days=${days}`
    ),
  events: (limit = 50, kind = "", model = "", key = "") =>
    req<{ time: string; kind: string; instance?: string; model?: string; key?: string }[]>(
      `/events?limit=${limit}${kind ? "&kind=" + encodeURIComponent(kind) : ""}${model ? "&model=" + encodeURIComponent(model) : ""}${key ? "&key=" + encodeURIComponent(key) : ""}`
    ),
  doctor: () =>
    req<{ ok: boolean; checks: { name: string; status: string; detail: string }[] }>("/doctor"),
  gpu: () => req<{ gpus: GPUInfo[] }>("/gpu"),
  disk: () =>
    req<{ dir: string; modelsBytes: number; modelsCount: number; freeBytes?: number; totalBytes?: number }>(
      "/disk",
    ),
  backups: () => req<{ name: string; size: number; created: string }[]>("/backups"),
  createBackup: (label?: string) =>
    req<{ name: string; size: number; created: string }>("/backups", {
      method: "POST",
      body: label ? JSON.stringify({ label }) : undefined,
    }),
  deleteBackup: (name: string) => req<{ ok: boolean }>(`/backups/${encodeURIComponent(name)}`, { method: "DELETE" }),
  pruneBackups: (keep: number) =>
    req<{ keep: number; deleted: string[] }>("/backups/prune", { method: "POST", body: JSON.stringify({ keep }) }),
  verifyBackup: (name: string) =>
    req<{ ok: boolean; entries?: number; error?: string }>(`/backups/${encodeURIComponent(name)}/verify`, { method: "POST" }),
  restoreStoredBackup: (name: string) =>
    req<{ ok: boolean; files: number }>(`/backups/${encodeURIComponent(name)}/restore`, { method: "POST" }),
  requests: (
    limit = 100,
    filters: { model?: string; status?: string; minMs?: number; stream?: string; key?: string; instance?: string; since?: string; until?: string } = {}
  ) => {
    const q = new URLSearchParams({ limit: String(limit) });
    if (filters.model) q.set("model", filters.model);
    if (filters.status) q.set("status", filters.status);
    if (filters.minMs) q.set("minMs", String(filters.minMs));
    if (filters.stream) q.set("stream", filters.stream);
    if (filters.key) q.set("key", filters.key);
    if (filters.instance) q.set("instance", filters.instance);
    if (filters.since) q.set("since", filters.since);
    if (filters.until) q.set("until", filters.until);
    return req<RequestLogEntry[]>(`/requests?${q.toString()}`);
  },
  requestById: (id: string) => req<RequestLogEntry>(`/requests/${encodeURIComponent(id)}`),
  aliases: () => req<ModelAlias[]>("/aliases"),
  createAlias: (alias: string, model: string) =>
    req<ModelAlias>("/aliases", { method: "POST", body: JSON.stringify({ alias, model }) }),
  deleteAlias: (alias: string) =>
    req<{ ok: boolean }>(`/aliases/${encodeURIComponent(alias)}`, { method: "DELETE" }),
  batch: (
    key: string,
    body: { model: string; prompts: string[]; system?: string; maxTokens?: number; temperature?: number; seed?: number; topP?: number; freqPenalty?: number; presPenalty?: number; concurrency?: number }
  ) =>
    fetch("/api/v1/batch", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(key ? { Authorization: `Bearer ${key}` } : {}),
      },
      body: JSON.stringify(body),
    }).then(async (r) => {
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error((d as any).error || `batch failed (${r.status})`);
      return d as { ok: number; failed: number; concurrency: number; results: BatchResult[] };
    }),
  agentApprove: (id: string, approve: boolean) =>
    req<{ ok: boolean }>("/agent/approve", { method: "POST", body: JSON.stringify({ id, approve }) }),
  structured: (
    key: string,
    body: {
      model: string;
      messages: { role: string; content: string }[];
      schema: Record<string, unknown>;
      maxRetries?: number;
      maxTokens?: number;
      temperature?: number;
      seed?: number;
      topP?: number;
      freqPenalty?: number;
      presPenalty?: number;
    }
  ) =>
    fetch("/api/v1/structured", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(key ? { Authorization: `Bearer ${key}` } : {}),
      },
      body: JSON.stringify(body),
    }).then(async (r) => {
      const d = await r.json().catch(() => ({}));
      if (!r.ok && r.status !== 422) throw new Error((d as any).error || `structured failed (${r.status})`);
      return d as { ok: boolean; json?: unknown; attempts?: number; error?: string; raw?: string };
    }),
  mcpServers: () => req<MCPServer[]>("/mcp"),
  createMCPServer: (body: { name: string; command: string; args?: string[]; env?: Record<string, string> }) =>
    req<MCPServer>("/mcp", { method: "POST", body: JSON.stringify(body) }),
  deleteMCPServer: (id: string) => req<{ ok: boolean }>(`/mcp/${encodeURIComponent(id)}`, { method: "DELETE" }),
  setMCPEnabled: (id: string, enabled: boolean) =>
    req<MCPServer>(`/mcp/${encodeURIComponent(id)}/enabled`, { method: "POST", body: JSON.stringify({ enabled }) }),
  testMCPServer: (id: string) =>
    req<{ tools: MCPToolDef[] }>(`/mcp/${encodeURIComponent(id)}/test`, { method: "POST" }),
  chats: () => req<ChatSummary[]>("/chats"),
  getChat: (id: string) => req<ChatConversation>(`/chats/${encodeURIComponent(id)}`),
  createChat: (c: { title?: string; model?: string; agentMode?: boolean; system?: string; messages: unknown[] }) =>
    req<ChatConversation>("/chats", { method: "POST", body: JSON.stringify(c) }),
  // Import an exported chat file verbatim (id/context stripped by caller):
  // the store mints a fresh id (F369, CLI twin F368).
  importChat: (c: Record<string, unknown>) =>
    req<ChatConversation>("/chats", { method: "POST", body: JSON.stringify(c) }),
  saveChat: (id: string, c: { title?: string; model?: string; agentMode?: boolean; system?: string; messages: unknown[] }) =>
    req<ChatConversation>(`/chats/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(c) }),
  deleteChat: (id: string) => req<{ ok: boolean }>(`/chats/${encodeURIComponent(id)}`, { method: "DELETE" }),
  pinChat: (id: string, pinned: boolean) =>
    req<{ ok: boolean }>(`/chats/${encodeURIComponent(id)}/pin`, { method: "POST", body: JSON.stringify({ pinned }) }),
  setChatTags: (id: string, tags: string[]) =>
    req<{ ok: boolean; tags: string[] }>(`/chats/${encodeURIComponent(id)}/tags`, {
      method: "PATCH",
      body: JSON.stringify({ tags }),
    }),
  forkChat: (id: string, body: { forkAt: number; title?: string }) =>
    req<ChatConversation>(`/chats/${encodeURIComponent(id)}/fork`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
  searchChats: (q: string, limit = 20, maxMatches = 5) =>
    req<SearchChatResult[]>(`/chats/search?q=${encodeURIComponent(q)}&limit=${limit}&maxMatches=${maxMatches}`),
  tools: () => req<ToolRecord[]>("/tools"),
  installTool: (body: { url?: string; dir?: string; id?: string }) =>
    req<ToolRecord>("/tools/install", { method: "POST", body: JSON.stringify(body) }),
  setToolEnabled: (id: string, enabled: boolean) =>
    req<{ ok: boolean }>(`/tools/${encodeURIComponent(id)}/enabled`, { method: "POST", body: JSON.stringify({ enabled }) }),
  setToolSandbox: (id: string, sandbox: boolean) =>
    req<{ ok: boolean }>(`/tools/${encodeURIComponent(id)}/sandbox`, { method: "POST", body: JSON.stringify({ sandbox }) }),
  runTool: (id: string, args: Record<string, unknown>, timeoutSec = 60) =>
    req<ToolRunResult>(`/tools/${encodeURIComponent(id)}/run`, { method: "POST", body: JSON.stringify({ args, timeoutSec }) }),
  deleteTool: (id: string) => req<{ ok: boolean }>(`/tools/${encodeURIComponent(id)}`, { method: "DELETE" }),
  skills: () => req<SkillRecord[]>("/skills"),
  createSkill: (body: { short_name: string; name?: string; description?: string; prompt_template: string; tags?: string[] }) =>
    req<SkillRecord>("/skills", { method: "POST", body: JSON.stringify(body) }),
  getSkill: (id: string) => req<SkillRecord>(`/skills/${encodeURIComponent(id)}`),
  updateSkill: (id: string, body: { short_name?: string; name?: string; description?: string; prompt_template?: string; tags?: string[] }) =>
    req<SkillRecord>(`/skills/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(body) }),
  deleteSkill: (id: string) => req<{ ok: boolean }>(`/skills/${encodeURIComponent(id)}`, { method: "DELETE" }),
  setSkillEnabled: (id: string, enabled: boolean) =>
    req<{ ok: boolean }>(`/skills/${encodeURIComponent(id)}/enabled`, { method: "POST", body: JSON.stringify({ enabled }) }),
  memory: () => req<MemoryEntry[]>("/memory"),
  createMemory: (body: { content: string; tags?: string[]; source?: string }) =>
    req<MemoryEntry>("/memory", { method: "POST", body: JSON.stringify(body) }),
  getMemory: (id: string) => req<MemoryEntry>(`/memory/${encodeURIComponent(id)}`),
  updateMemory: (id: string, body: { content?: string; tags?: string[]; source?: string }) =>
    req<MemoryEntry>(`/memory/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(body) }),
  deleteMemory: (id: string) => req<{ ok: boolean }>(`/memory/${encodeURIComponent(id)}`, { method: "DELETE" }),
  searchMemory: (q: string, limit = 10) =>
    req<MemoryEntry[]>(`/memory/search?q=${encodeURIComponent(q)}&limit=${limit}`),
  files: () => req<FileEntry[]>("/files"),
  setFileTags: (id: string, tags: string[]) =>
    req<FileEntry>(`/files/${encodeURIComponent(id)}/tags`, {
      method: "PATCH",
      body: JSON.stringify({ tags }),
    }),
  uploadFile: (formData: FormData) =>
    fetch("/api/v1/files", {
      method: "POST",
      body: formData,
    }).then(async (r) => {
      if (!r.ok) {
        const t = await r.text();
        throw new Error(t || `HTTP ${r.status}`);
      }
      return r.json() as Promise<FileEntry>;
    }),
  getFileContent: (id: string) =>
    fetch(`/api/v1/files/${encodeURIComponent(id)}/content`, {
      method: "GET",
    }).then(async (r) => {
      if (!r.ok) {
        const t = await r.text();
        throw new Error(t || `HTTP ${r.status}`);
      }
      return r.text();
    }),
  deleteFile: (id: string) => req<{ ok: boolean }>(`/files/${encodeURIComponent(id)}`, { method: "DELETE" }),
  knowledgeBases: () => req<KB[]>("/knowledge-bases"),
  createKB: (body: { name: string; description?: string; tags?: string[] }) =>
    req<KB>("/knowledge-bases", { method: "POST", body: JSON.stringify(body) }),
  getKB: (id: string) => req<KB>(`/knowledge-bases/${encodeURIComponent(id)}`),
  updateKB: (id: string, body: { name?: string; description?: string }) =>
    req<KB>(`/knowledge-bases/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(body) }),
  deleteKB: (id: string) => req<{ ok: boolean }>(`/knowledge-bases/${encodeURIComponent(id)}`, { method: "DELETE" }),
  setKBTags: (id: string, tags: string[]) =>
    req<{ ok: boolean; tags: string[] }>(`/knowledge-bases/${encodeURIComponent(id)}/tags`, { method: "PATCH", body: JSON.stringify({ tags }) }),
  listKBDocs: (kbId: string) => req<KBDoc[]>(`/knowledge-bases/${encodeURIComponent(kbId)}/documents`),
  uploadKBDoc: (kbId: string, formData: FormData) =>
    fetch(`/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/documents`, {
      method: "POST",
      body: formData,
    }).then(async (r) => {
      if (!r.ok) {
        const t = await r.text();
        throw new Error(t || `HTTP ${r.status}`);
      }
      return r.json() as Promise<KBDoc>;
    }),
  getKBDocContent: (kbId: string, docId: string) =>
    fetch(`/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/documents/${encodeURIComponent(docId)}/content`, {
      method: "GET",
    }).then(async (r) => {
      if (!r.ok) {
        const t = await r.text();
        throw new Error(t || `HTTP ${r.status}`);
      }
      return r.text();
    }),
  deleteKBDoc: (kbId: string, docId: string) =>
    req<{ ok: boolean }>(`/knowledge-bases/${encodeURIComponent(kbId)}/documents/${encodeURIComponent(docId)}`, { method: "DELETE" }),
  searchKBDocs: (kbId: string, body: { q: string; limit?: number }) =>
    req<KBSearchResult[]>(`/knowledge-bases/${encodeURIComponent(kbId)}/search`, { method: "POST", body: JSON.stringify(body) }),
  searchAllKBs: (body: { q: string; limit?: number }) =>
    req<{ query: string; results: KBAllSearchResult[] }>("/knowledge-bases/search", { method: "POST", body: JSON.stringify(body) }),
  setKBEmbed: (kbId: string, body: { url?: string; model?: string; apiKey?: string }) =>
    req<KB>(`/knowledge-bases/${encodeURIComponent(kbId)}/embed`, { method: "PUT", body: JSON.stringify(body) }),
  costSummary: () => req<CostSummary>("/cost"),
  costKeys: () => req<Record<string, CostCounter>>("/cost/keys"),
  costModels: () => req<Record<string, CostCounter>>("/cost/models"),
  schedules: () => req<ScheduleSpec[]>("/scheduler"),
  createSchedule: (body: { name: string; cron_expr: string; kind?: string; model: string; prompt?: string; system?: string; kb?: string; instance?: string; keep?: number; webhook_url?: string; enabled?: boolean; tags?: string[]; temperature?: number; max_tokens?: number; seed?: number }) =>
    req<ScheduleSpec>("/scheduler", { method: "POST", body: JSON.stringify(body) }),
  getSchedule: (id: string) => req<ScheduleSpec>(`/scheduler/${encodeURIComponent(id)}`),
  updateSchedule: (id: string, body: { name?: string; cron_expr?: string; model?: string; prompt?: string; kb?: string; enabled?: boolean; webhook_url?: string; temperature?: number; max_tokens?: number; seed?: number }) =>
    req<ScheduleSpec>(`/scheduler/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(body) }),
  deleteSchedule: (id: string) => req<{ ok: boolean }>(`/scheduler/${encodeURIComponent(id)}`, { method: "DELETE" }),
  runSchedule: (id: string) => req<{ ok: boolean; started: string }>(`/scheduler/${encodeURIComponent(id)}/run`, { method: "POST" }),
  schedulerRuns: (limit = 0, task = "") =>
    req<ScheduleRun[]>(
      `/scheduler/runs${limit ? "?limit=" + limit : ""}${task ? (limit ? "&" : "?") + "task=" + encodeURIComponent(task) : ""}`
    ),
  setScheduleEnabled: (id: string, enabled: boolean) =>
    req<{ ok: boolean }>(`/scheduler/${encodeURIComponent(id)}/enabled`, { method: "POST", body: JSON.stringify({ enabled }) }),
  setScheduleTags: (id: string, tags: string[]) =>
    req<{ ok: boolean; tags: string[] }>(`/scheduler/${encodeURIComponent(id)}/tags`, {
      method: "PATCH",
      body: JSON.stringify({ tags }),
    }),
  prompts: () => req<PromptTemplate[]>("/prompts"),
  createPrompt: (body: { id: string; name: string; category?: string; description?: string; content: string; tags?: string[] }) =>
    req<PromptTemplate>("/prompts", { method: "POST", body: JSON.stringify(body) }),
  deletePrompt: (id: string) =>
    req<{ ok: boolean }>(`/prompts/${encodeURIComponent(id)}`, { method: "DELETE" }),
  updatePrompt: (id: string, body: { name?: string; category?: string; description?: string; content?: string; tags?: string[] }) =>
    req<PromptTemplate>(`/prompts/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(body) }),
  setPromptTags: (id: string, tags: string[]) =>
    req<PromptTemplate>(`/prompts/${encodeURIComponent(id)}/tags`, { method: "PATCH", body: JSON.stringify({ tags }) }),
  promptCategories: () => req<string[]>("/prompts/categories"),
  prompt: (id: string) => req<PromptTemplate>(`/prompts/${encodeURIComponent(id)}`),
  presets: () => req<PresetSpec[]>("/presets"),
  savePreset: (p: PresetSpec) => req<PresetSpec>("/presets", { method: "POST", body: JSON.stringify(p) }),
  deletePreset: (name: string) => req<{ ok: boolean }>(`/presets/${encodeURIComponent(name)}`, { method: "DELETE" }),
  startPreset: (name: string, overrides?: { port?: number; vars?: Record<string, string> }) =>
    req<Instance>(`/presets/${encodeURIComponent(name)}/start`, { method: "POST", body: JSON.stringify(overrides ?? {}) }),
  catalog: () => req<CatalogEntry[]>("/catalog"),
  installCatalogModel: (id: string) =>
    req<{ job: string; model: string }>("/catalog/install", { method: "POST", body: JSON.stringify({ id }) }),
  autoStart: () => req<AutoStartSpec[]>("/autostart"),
  setAutoStart: (body: AutoStartSpec) =>
    req<{ ok: boolean }>("/autostart", { method: "POST", body: JSON.stringify(body) }),
  clearAutoStart: (model: string) =>
    req<{ ok: boolean }>(`/autostart/${encodeURIComponent(model)}`, { method: "DELETE" }),
  jobs: () => req<Job[]>("/jobs"),
  clearJobs: () => req<{ cleared: number }>("/jobs", { method: "DELETE" }),
  keys: () => req<ApiKey[]>("/keys"),
  createKey: (name: string, tokenQuota?: number, allowedModels?: string[], maxConcurrent?: number, maxRequests?: number, rateRPS?: number, rateBurst?: number, expiresInDays?: number, dailyCostBudgetUSD?: number, note?: string, tags?: string[]) =>
    req<{ id: string; name: string; key: string; prefix: string }>(`/keys`, {
      method: "POST",
      body: JSON.stringify({ name, tokenQuota: tokenQuota ?? 0, allowedModels: allowedModels ?? [], maxConcurrent: maxConcurrent ?? 0, maxRequests: maxRequests ?? 0, rateRPS: rateRPS ?? 0, rateBurst: rateBurst ?? 0, expiresInDays: expiresInDays ?? 0, dailyCostBudgetUSD: dailyCostBudgetUSD ?? 0, note: note ?? "", ...(tags && tags.length ? { tags } : {}) }),
    }),
  updateKey: (id: string, body: { allowedModels?: string[]; rateRPS?: number; rateBurst?: number; expiresInDays?: number; dailyCostBudgetUSD?: number; name?: string; note?: string; tokenQuota?: number; maxRequests?: number }) =>
    req<{ ok: boolean; allowedModels: string[] }>(`/keys/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  setKeyTags: (id: string, tags: string[]) =>
    req<{ ok: boolean; tags: string[] }>(`/keys/${encodeURIComponent(id)}/tags`, {
      method: "PATCH",
      body: JSON.stringify({ tags }),
    }),
  revokeKey: (id: string) => req<{ ok: boolean }>(`/keys/${encodeURIComponent(id)}`, { method: "DELETE" }),
  resetKeyUsage: (id: string) =>
    req<{ ok: boolean }>(`/keys/${encodeURIComponent(id)}/reset-usage`, { method: "POST" }),
  setKeyDisabled: (id: string, disabled: boolean) =>
    req<{ ok: boolean }>(`/keys/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify({ disabled }),
    }),
  rotateKey: (id: string) =>
    req<{ id: string; key: string; prefix: string }>(`/keys/${encodeURIComponent(id)}/rotate`, {
      method: "POST",
      body: "{}",
    }),
  pruneKeys: (idleDays?: number) =>
    req<{ ok: boolean; removed: string[] }>(`/keys/prune${idleDays ? `?idle=${idleDays}` : ""}`, { method: "POST" }),
  daemonLog: (bytes = 8000) =>
    req<{ size: number; logs: string }>(`/logs/daemon?bytes=${bytes}`),
  settings: () => req<Settings>("/settings"),
  updateSettings: (body: { requireAuth?: boolean; idleUnloadMin?: number; notifyWebhook?: string; notifyWebhookSecret?: string; notifyEvents?: string[]; allowCORS?: boolean; startupTimeoutSec?: number; startOnDemand?: boolean; maxBodyMB?: number; rateRPS?: number; rateBurst?: number; usageRetentionDays?: number; logLevel?: string; backupKeep?: number }) =>
    req<Settings>("/settings", { method: "PATCH", body: JSON.stringify(body) }),
  testNotifyWebhook: (event?: string, message?: string) =>
    req<{ ok: boolean; error?: string }>("/settings/notify-test", {
      method: "POST",
      body: JSON.stringify({
        ...(event ? { event } : {}),
        ...(message ? { message } : {}),
      }),
    }),
  webhookLog: () =>
    req<WebhookDelivery[]>("/webhook/log"),
  retryWebhook: (id: string) =>
    req<{ ok: boolean; error?: string }>("/webhook/retry", { method: "POST", body: JSON.stringify({ id }) }),
  restoreBackup: (file: File) => {
    const fd = new FormData();
    fd.append("file", file);
    return fetch("/api/v1/restore", { method: "POST", body: fd }).then(async (r) => {
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error((d as any).error || `restore failed (${r.status})`);
      return d as { ok: boolean; files: number };
    });
  },
};

// Chat via the OpenAI gateway (streaming SSE). Returns an async iterator of
// content deltas.
export async function* chatStream(
  key: string,
  model: string,
  messages: { role: string; content: string }[],
  opts: {
    maxTokens?: number;
    temperature?: number;
    seed?: number; // reproducible sampling seed, passed through to the backend (F793)
    topP?: number; // nucleus sampling top_p, passed through to the backend (F797)
    freqPenalty?: number; // frequency penalty, passed through to the backend (F803)
    presPenalty?: number; // presence penalty, passed through to the backend (F803)
    onUsage?: (u: { prompt: number; completion: number; total: number }) => void;
    signal?: AbortSignal; // cancel the stream mid-flight (F754)
  } = {}
): AsyncGenerator<string> {
  const body: Record<string, unknown> = {
    model,
    messages,
    stream: true,
    // Ask OpenAI-compatible upstreams to send a final usage chunk (F078).
    stream_options: { include_usage: true },
  };
  if (opts.maxTokens) body.max_tokens = opts.maxTokens;
  if (opts.temperature !== undefined) body.temperature = opts.temperature;
  if (opts.seed !== undefined) body.seed = opts.seed;
  if (opts.topP !== undefined) body.top_p = opts.topP;
  if (opts.freqPenalty !== undefined) body.frequency_penalty = opts.freqPenalty;
  if (opts.presPenalty !== undefined) body.presence_penalty = opts.presPenalty;
  const r = await fetch("/v1/chat/completions", {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${key}` },
    body: JSON.stringify(body),
    signal: opts.signal,
  });
  if (!r.ok || !r.body) {
    const t = await r.text();
    let msg = t;
    try {
      msg = (JSON.parse(t).error?.message as string) || t;
    } catch { /* keep raw */ }
    throw new ApiError(r.status, msg);
  }
  const reader = r.body.getReader();
  const dec = new TextDecoder();
  let buf = "";
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    let idx: number;
    while ((idx = buf.indexOf("\n")) >= 0) {
      const line = buf.slice(0, idx).trim();
      buf = buf.slice(idx + 1);
      if (!line.startsWith("data:")) continue;
      const data = line.slice(5).trim();
      if (data === "[DONE]") return;
      try {
        const j = JSON.parse(data);
        if (j.usage && opts.onUsage) {
          const p = (j.usage.prompt_tokens as number) ?? 0;
          const c = (j.usage.completion_tokens as number) ?? 0;
          opts.onUsage({ prompt: p, completion: c, total: (j.usage.total_tokens as number) ?? p + c });
        }
        const delta = j.choices?.[0]?.delta?.content;
        if (delta) yield delta;
      } catch { /* partial line */ }
    }
  }
}

export interface AgentToolEvent {
  type: "tool";
  step: number;
  name: string;
  query: string;
  results?: { title: string; url: string; snippet: string }[];
  error?: string;
}
export interface AgentEvent {
  type: "delta" | "tool" | "done" | "error" | "system" | "approval";
  content?: string;
  step?: number;
  name?: string;
  query?: string;
  results?: { title: string; url: string; snippet: string }[];
  error?: string;
  steps?: number;
  id?: string; // approval token
}

/** Agent chat: server-side loop with web_search tool. Streams AgentEvents. */
export async function* agentStream(
  key: string,
  body: {
    model: string;
    messages: { role: string; content: string }[];
    maxSteps?: number;
    maxTokens?: number; // conversation max-tokens, honored in agent mode (F829)
    temperature?: number;
    requireApproval?: boolean;
    tools?: string[]; // built-in tools to enable; [] = none, omit = all (F820)
  },
  opts: { signal?: AbortSignal } = {} // cancel the stream mid-flight (F754)
): AsyncGenerator<AgentEvent> {
  const r = await fetch("/api/v1/chat/agent", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(key ? { Authorization: `Bearer ${key}` } : {}),
    },
    body: JSON.stringify(body),
    signal: opts.signal,
  });
  if (!r.ok || !r.body) {
    const t = await r.text();
    let msg = t;
    try {
      msg = (JSON.parse(t).error?.message as string) || t;
    } catch { /* keep raw */ }
    throw new ApiError(r.status, msg);
  }
  const reader = r.body.getReader();
  const dec = new TextDecoder();
  let buf = "";
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    let idx: number;
    while ((idx = buf.indexOf("\n")) >= 0) {
      const line = buf.slice(0, idx).trim();
      buf = buf.slice(idx + 1);
      if (!line.startsWith("data:")) continue;
      const data = line.slice(5).trim();
      try {
        yield JSON.parse(data) as AgentEvent;
      } catch { /* partial line */ }
    }
  }
}
