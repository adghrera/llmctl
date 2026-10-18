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
