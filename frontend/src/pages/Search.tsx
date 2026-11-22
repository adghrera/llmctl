import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, ApiKey, Instance, KB, MemoryEntry, Model, PromptTemplate, SearchChatResult } from "../api/client";

// Global search across local resources (F283): models, chats, memory,
// knowledge bases, prompt templates, instances and API keys (F364). Pure
// client-side fan-out over the existing endpoints.

export default function Search() {
  const [params] = useSearchParams();
  const [q, setQ] = useState(params.get("q") || "");
  const [running, setRunning] = useState(false);
  const [models, setModels] = useState<Model[]>([]);
  const [chats, setChats] = useState<SearchChatResult[]>([]);
  const [memories, setMemories] = useState<MemoryEntry[]>([]);
  const [kbs, setKbs] = useState<KB[]>([]);
  const [prompts, setPrompts] = useState<PromptTemplate[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [searched, setSearched] = useState(false);

  const run = async (query: string) => {
    const t = query.trim();
    if (!t) return;
    setRunning(true);
    setSearched(true);
    const low = t.toLowerCase();
    const [m, c, mem, kb, pr, inst, ks] = await Promise.allSettled([
      api.models().then((all) =>
        all.filter(
          (x) =>
            x.id.toLowerCase().includes(low) ||
            x.backendId.toLowerCase().includes(low) ||
            x.path.toLowerCase().includes(low)
        )
      ),
      api.searchChats(t, 10, 2),
      api.searchMemory(t, 10),
      api.knowledgeBases().then((all) =>
        all.filter(
          (k) =>
            k.kb.name.toLowerCase().includes(low) ||
            (k.kb.description || "").toLowerCase().includes(low)
        )
      ),
      api.prompts().then((all) =>
        all.filter(
          (p) =>
            p.id.toLowerCase().includes(low) ||
            p.name.toLowerCase().includes(low) ||
            (p.description || "").toLowerCase().includes(low) ||
            (p.category || "").toLowerCase().includes(low) ||
            p.content.toLowerCase().includes(low)
        )
      ),
      api.instances().then((all) =>
        all.filter(
          (i) =>
            i.id.toLowerCase().includes(low) ||
            i.modelId.toLowerCase().includes(low) ||
            i.backendId.toLowerCase().includes(low) ||
            (i.note || "").toLowerCase().includes(low)
        )
      ),
      api.keys().then((all) =>
        all.filter(
          (k) =>
            k.name.toLowerCase().includes(low) ||
            k.prefix.toLowerCase().includes(low)
        )
      ),
    ]);
    setModels(m.status === "fulfilled" ? m.value : []);
    setChats(c.status === "fulfilled" ? c.value : []);
    setMemories(mem.status === "fulfilled" ? mem.value : []);
    setKbs(kb.status === "fulfilled" ? kb.value : []);
    setPrompts(pr.status === "fulfilled" ? pr.value : []);
    setInstances(inst.status === "fulfilled" ? inst.value : []);
    setKeys(ks.status === "fulfilled" ? ks.value : []);
    setRunning(false);
  };

  useEffect(() => {
    if (params.get("q")) run(params.get("q")!);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const total =
    models.length + chats.length + memories.length + kbs.length + prompts.length +
    instances.length + keys.length;

  // One CSV across every matched section (kind,name,detail), F611.
  const exportCsv = () => {
    const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
    const rows = ["kind,name,detail"];
    models.forEach((m) => rows.push(["model", m.id, `${m.backendId}${m.running ? " running" : ""}`].map(esc).join(",")));
    chats.forEach((c) => rows.push(["chat", c.title, c.model || "no model"].map(esc).join(",")));
    memories.forEach((m) => rows.push(["memory", m.entry.content.replace(/\s+/g, " "), (m.entry.tags || []).join(" ")].map(esc).join(",")));
    prompts.forEach((p) => rows.push(["prompt", p.name, `${p.category}${p.custom ? " custom" : ""}`].map(esc).join(",")));
    kbs.forEach((k) => rows.push(["kb", k.kb.name, `${k.kb.doc_count} doc(s)`].map(esc).join(",")));
    instances.forEach((i) => rows.push(["instance", i.id, `${i.modelId} ${i.backendId} ${i.status}`].map(esc).join(",")));
    keys.forEach((k) => rows.push(["key", k.name || k.prefix, `${k.prefix}${k.disabled ? " disabled" : ""}`].map(esc).join(",")));
    const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `llmctl-search-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
  };

  return (
    <>
      <div className="card">
        <h2>Search</h2>
        <div className="row" style={{ gap: 8 }}>
          <input
            style={{ flex: 1 }}
            placeholder="Search models, chats, memory, knowledge bases, prompts, instances and keys…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && run(q)}
          />
          <button className="btn primary" disabled={running || !q.trim()} onClick={() => run(q)}>
            {running ? "Searching…" : "Search"}
          </button>
          {searched && total > 0 && (
            <button className="btn" title="download every match as one CSV (kind,name,detail)" onClick={exportCsv}>
              Export CSV
            </button>
          )}
        </div>
        {searched && !running && total === 0 && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No matches for “{q.trim()}”.
          </div>
        )}
      </div>

      {models.length > 0 && (
        <div className="card">
          <h2>Models ({models.length})</h2>
          {models.map((m) => (
            <div className="row" key={m.id} style={{ gap: 8, marginBottom: 4 }}>
              <Link to={`/models?q=${encodeURIComponent(m.id)}`}>{m.id}</Link>
              <span className="muted small">
                {m.backendId}
                {m.running ? " · running" : ""}
              </span>
            </div>
          ))}
        </div>
      )}

      {chats.length > 0 && (
        <div className="card">
          <h2>Chats ({chats.length})</h2>
          {chats.map((c) => (
            <div key={c.conversationId} style={{ marginBottom: 8 }}>
              <Link to={`/chat?chat=${c.conversationId}`}>{c.title}</Link>
              <span className="muted small"> · {c.model || "no model"}</span>
              {c.matches?.slice(0, 2).map((m, i) => (
                <div className="muted small" key={i} style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  [{m.role}] {m.content.replace(/\s+/g, " ").slice(0, 110)}
                </div>
              ))}
            </div>
          ))}
        </div>
      )}

      {memories.length > 0 && (
        <div className="card">
          <h2>Memory ({memories.length})</h2>
          {memories.map((m) => (
            <div className="row" key={m.id} style={{ gap: 8, marginBottom: 4 }}>
              <Link to={`/memory?q=${encodeURIComponent(m.entry.content.slice(0, 40))}`}>
                {m.entry.content.replace(/\s+/g, " ").slice(0, 90)}
              </Link>
              {m.entry.tags && m.entry.tags.length > 0 && (
                <span className="muted small">[{m.entry.tags.join(", ")}]</span>
              )}
            </div>
          ))}
        </div>
      )}

      {prompts.length > 0 && (
        <div className="card">
          <h2>Prompts ({prompts.length})</h2>
          {prompts.map((p) => (
            <div className="row" key={p.id} style={{ gap: 8, marginBottom: 4 }}>
              <Link to={`/prompts?q=${encodeURIComponent(p.id)}`}>{p.name}</Link>
              <span className="muted small">
                {p.category}
                {p.custom ? " · custom" : ""}
                {p.description ? ` · ${p.description.slice(0, 60)}` : ""}
              </span>
            </div>
          ))}
        </div>
      )}

      {kbs.length > 0 && (
        <div className="card">
          <h2>Knowledge Bases ({kbs.length})</h2>
          {kbs.map((k) => (
            <div className="row" key={k.id} style={{ gap: 8, marginBottom: 4 }}>
              <Link to={`/knowledge-bases?q=${encodeURIComponent(k.kb.name)}`}>{k.kb.name}</Link>
              <span className="muted small">
                {k.kb.doc_count} doc(s)
                {k.kb.description ? ` · ${k.kb.description.slice(0, 60)}` : ""}
              </span>
            </div>
          ))}
        </div>
      )}
      {instances.length > 0 && (
        <div className="card">
          <h2>Instances ({instances.length})</h2>
          {instances.map((i) => (
            <div className="row" key={i.id} style={{ gap: 8, marginBottom: 4 }}>
              <Link to="/instances">{i.id}</Link>
              <span className="muted small">
                {i.modelId} · {i.backendId} · {i.status}
                {i.note ? ` · ${i.note.slice(0, 40)}` : ""}
              </span>
            </div>
          ))}
        </div>
      )}

      {keys.length > 0 && (
        <div className="card">
          <h2>API Keys ({keys.length})</h2>
          {keys.map((k) => (
            <div className="row" key={k.id} style={{ gap: 8, marginBottom: 4 }}>
              <Link to="/keys">{k.name || k.prefix}</Link>
              <span className="muted small">
                {k.prefix}…{k.disabled ? " · disabled" : ""}
              </span>
            </div>
          ))}
        </div>
      )}
    </>
  );
}
