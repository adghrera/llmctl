import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, KB, MemoryEntry, Model, PromptTemplate, SearchChatResult } from "../api/client";

// Global search across local resources (F283): models, chats, memory,
// knowledge bases and prompt templates. Pure client-side fan-out over the
// existing endpoints.

export default function Search() {
  const [params] = useSearchParams();
  const [q, setQ] = useState(params.get("q") || "");
  const [running, setRunning] = useState(false);
  const [models, setModels] = useState<Model[]>([]);
  const [chats, setChats] = useState<SearchChatResult[]>([]);
  const [memories, setMemories] = useState<MemoryEntry[]>([]);
  const [kbs, setKbs] = useState<KB[]>([]);
  const [prompts, setPrompts] = useState<PromptTemplate[]>([]);
  const [searched, setSearched] = useState(false);

  const run = async (query: string) => {
    const t = query.trim();
    if (!t) return;
    setRunning(true);
    setSearched(true);
    const low = t.toLowerCase();
    const [m, c, mem, kb, pr] = await Promise.allSettled([
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
    ]);
    setModels(m.status === "fulfilled" ? m.value : []);
    setChats(c.status === "fulfilled" ? c.value : []);
    setMemories(mem.status === "fulfilled" ? mem.value : []);
    setKbs(kb.status === "fulfilled" ? kb.value : []);
    setPrompts(pr.status === "fulfilled" ? pr.value : []);
    setRunning(false);
  };

  useEffect(() => {
    if (params.get("q")) run(params.get("q")!);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const total = models.length + chats.length + memories.length + kbs.length + prompts.length;

  return (
    <>
      <div className="card">
        <h2>Search</h2>
        <div className="row" style={{ gap: 8 }}>
          <input
            style={{ flex: 1 }}
            placeholder="Search models, chats, memory, knowledge bases and prompts…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && run(q)}
          />
          <button className="btn primary" disabled={running || !q.trim()} onClick={() => run(q)}>
            {running ? "Searching…" : "Search"}
          </button>
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
              <Link to={`/prompts`}>{p.name}</Link>
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
    </>
  );
}
