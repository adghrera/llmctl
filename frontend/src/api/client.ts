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
  vars?: Record<string, string>;
  lastError?: string;
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

export interface PromptTemplate {
  id: string;
  name: string;
  description: string;
  category: string;
  content: string;
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
  messages: number;
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
  models: Record<string, { requests: number; errors: number; prompt_tokens: number; completion_tokens: number }>;
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
}

export interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  createdAt: string;
}

export interface Settings {
  requireAuth: boolean;
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
  backends: () => req<Backend[]>("/backends"),
  installBackend: (id: string) =>
    req<{ job: string; status: string }>(`/backends/${encodeURIComponent(id)}/install`, { method: "POST" }),
  uninstallBackend: (id: string) =>
    req<{ ok: boolean }>(`/backends/${encodeURIComponent(id)}/uninstall`, { method: "POST" }),
  addPlugin: (plugin: object) => req<{ ok: boolean }>("/backends", { method: "POST", body: JSON.stringify(plugin) }),
  removePlugin: (id: string) => req<{ ok: boolean }>(`/backends/${encodeURIComponent(id)}`, { method: "DELETE" }),
  models: () => req<Model[]>("/models"),
  installModel: (body: { repo: string; file?: string; revision?: string; backend?: string; id?: string }) =>
    req<{ job: string; status: string; model: string }>("/models/install", { method: "POST", body: JSON.stringify(body) }),
  removeModel: (id: string) => req<{ ok: boolean }>(`/models/${encodeURIComponent(id)}`, { method: "DELETE" }),
  setModelCtx: (id: string, ctxSize: number) =>
    req<{ ok: boolean; restartRequired?: boolean }>(`/models/${encodeURIComponent(id)}/ctx`, { method: "PUT", body: JSON.stringify({ ctxSize }) }),
  hfSearch: (q: string, limit = 15) =>
    req<Array<{ modelId?: string; id?: string; downloads?: number }>>(`/hf/search?q=${encodeURIComponent(q)}&limit=${limit}`),
  hfFiles: (repo: string, revision = "main") =>
    req<Array<{ Path: string; SizeBytes: number; OID: string }>>(`/hf/files?repo=${encodeURIComponent(repo)}&revision=${encodeURIComponent(revision)}`),
  instances: () => req<Instance[]>("/instances"),
  startInstance: (body: { model: string; backend?: string; instance?: string; port?: number; vars?: Record<string, string> }) =>
    req<Instance>("/instances", { method: "POST", body: JSON.stringify(body) }),
  stopInstance: (id: string) => req<{ ok: boolean }>(`/instances/${encodeURIComponent(id)}`, { method: "DELETE" }),
  instanceLogs: (id: string, stream: "stdout" | "stderr" = "stderr", bytes = 65536) =>
    req<InstanceLogTail>(`/instances/${encodeURIComponent(id)}/logs?stream=${stream}&bytes=${bytes}`),
  metrics: () => req<GatewayMetrics>("/metrics"),
  chats: () => req<ChatSummary[]>("/chats"),
  getChat: (id: string) => req<ChatConversation>(`/chats/${encodeURIComponent(id)}`),
  createChat: (c: { title?: string; model?: string; agentMode?: boolean; messages: unknown[] }) =>
    req<ChatConversation>("/chats", { method: "POST", body: JSON.stringify(c) }),
  saveChat: (id: string, c: { title?: string; model?: string; agentMode?: boolean; messages: unknown[] }) =>
    req<ChatConversation>(`/chats/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(c) }),
  deleteChat: (id: string) => req<{ ok: boolean }>(`/chats/${encodeURIComponent(id)}`, { method: "DELETE" }),
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
  prompts: () => req<PromptTemplate[]>("/prompts"),
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
  keys: () => req<ApiKey[]>("/keys"),
  createKey: (name: string) =>
    req<{ id: string; name: string; key: string; prefix: string }>(`/keys`, { method: "POST", body: JSON.stringify({ name }) }),
  revokeKey: (id: string) => req<{ ok: boolean }>(`/keys/${encodeURIComponent(id)}`, { method: "DELETE" }),
  settings: () => req<Settings>("/settings"),
  updateSettings: (body: { requireAuth?: boolean }) =>
    req<Settings>("/settings", { method: "PATCH", body: JSON.stringify(body) }),
};

// Chat via the OpenAI gateway (streaming SSE). Returns an async iterator of
// content deltas.
export async function* chatStream(
  key: string,
  model: string,
  messages: { role: string; content: string }[],
  opts: { maxTokens?: number; temperature?: number } = {}
): AsyncGenerator<string> {
  const body: Record<string, unknown> = { model, messages, stream: true };
  if (opts.maxTokens) body.max_tokens = opts.maxTokens;
  if (opts.temperature !== undefined) body.temperature = opts.temperature;
  const r = await fetch("/v1/chat/completions", {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${key}` },
    body: JSON.stringify(body),
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
  type: "delta" | "tool" | "done" | "error" | "system";
  content?: string;
  step?: number;
  name?: string;
  query?: string;
  results?: { title: string; url: string; snippet: string }[];
  error?: string;
  steps?: number;
}

/** Agent chat: server-side loop with web_search tool. Streams AgentEvents. */
export async function* agentStream(
  key: string,
  body: {
    model: string;
    messages: { role: string; content: string }[];
    maxSteps?: number;
    temperature?: number;
  }
): AsyncGenerator<AgentEvent> {
  const r = await fetch("/api/v1/chat/agent", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(key ? { Authorization: `Bearer ${key}` } : {}),
    },
    body: JSON.stringify(body),
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
