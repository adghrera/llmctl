import { useEffect, useRef, useState } from "react";
import { agentStream, chatStream, api, type AgentToolEvent, type ChatSummary } from "../api/client";
import { useStore } from "../state/store";
import { err, ok } from "../ui/toast";

interface Msg {
  role: "user" | "assistant";
  content: string;
  /** agent steps recorded while producing this message (searches run) */
  tools?: AgentToolEvent[];
}

const KEY_STORAGE = "llmctl.chat.key";
const AGENT_STORAGE = "llmctl.chat.agent";

export default function Chat() {
  const { instances } = useStore();
  const running = instances.filter((i) => i.status === "running");
  const [model, setModel] = useState("");
  const [key, setKey] = useState(() => localStorage.getItem(KEY_STORAGE) ?? "");
  const [agentMode, setAgentMode] = useState(() => localStorage.getItem(AGENT_STORAGE) === "1");
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [showKey, setShowKey] = useState(!key);
  const bottomRef = useRef<HTMLDivElement>(null);

  // ---- saved conversations (F025) ----
  const [convId, setConvId] = useState<string | null>(null);
  const [chats, setChats] = useState<ChatSummary[]>([]);
  const convIdRef = useRef<string | null>(null);
  convIdRef.current = convId;

  const loadChats = () => api.chats().then(setChats).catch(() => setChats([]));
  useEffect(() => {
    loadChats();
  }, []);

  // Persist current messages: create on first save, then update in place.
  const persist = async (messages: Msg[]) => {
    if (!messages.length) return;
    const payload = {
      model,
      agentMode,
      messages: messages.map((m) => ({
        role: m.role,
        content: m.content,
        tools: (m.tools || []).map((t) => ({ name: t.name, query: t.query, results: t.results })),
      })),
    };
    try {
      if (convIdRef.current) {
        await api.saveChat(convIdRef.current, payload);
      } else {
        const created = await api.createChat(payload);
        setConvId(created.id);
      }
      await loadChats();
    } catch (e) {
      err(`could not save conversation: ${e instanceof Error ? e.message : e}`);
    }
  };

  const newChat = () => {
    if (streaming) return;
    setMsgs([]);
    setConvId(null);
    setInput("");
  };

  const openChat = async (id: string) => {
    if (streaming) return;
    try {
      const c = await api.getChat(id);
      setMsgs(
        (c.messages || []).map((m) => ({
          role: m.role,
          content: m.content,
          tools: (m.tools || []).map((t, j) => ({
            type: "tool" as const,
            step: j + 1,
            name: t.name || "web_search",
            query: t.query || "",
            results: t.results as AgentToolEvent["results"],
          })),
        })),
      );
      setConvId(c.id);
      if (c.model) setModel(c.model);
      if (typeof c.agentMode === "boolean") setAgentMode(c.agentMode);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const removeChat = async (id: string) => {
    try {
      await api.deleteChat(id);
      if (id === convId) newChat();
      ok("conversation deleted");
      await loadChats();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  useEffect(() => {
    localStorage.setItem(KEY_STORAGE, key);
  }, [key]);
  useEffect(() => {
    localStorage.setItem(AGENT_STORAGE, agentMode ? "1" : "0");
  }, [agentMode]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [msgs]);

  // Default to the first running instance.
  useEffect(() => {
    if (!model && running.length) setModel(running[0].modelId);
  }, [running, model]);

  const updateLast = (fn: (m: Msg) => Msg) =>
    setMsgs((prev) => {
      const copy = [...prev];
      copy[copy.length - 1] = fn(copy[copy.length - 1]);
      return copy;
    });

  const send = async () => {
    const text = input.trim();
    if (!text || streaming) return;
    if (!key) {
      err("enter an API key first (create one on the API Keys page)");
      setShowKey(true);
      return;
    }
    if (!model) {
      err("no running instance — start one on the Instances page");
      return;
    }
    const next: Msg[] = [...msgs, { role: "user", content: text }];
    setMsgs(next);
    setInput("");
    setStreaming(true);
    const history = next.map((m) => ({ role: m.role, content: m.content }));
    let acc = "";
    const tools: AgentToolEvent[] = [];
    try {
      // Seed a single assistant message; each delta updates it in place.
      setMsgs((prev) => [...prev, { role: "assistant", content: "", tools: [] }]);
      if (agentMode) {
        for await (const ev of agentStream(key, { model, messages: history })) {
          if (ev.type === "delta" && ev.content) {
            acc += ev.content;
            updateLast((m) => ({ ...m, content: acc }));
          } else if (ev.type === "tool") {
            tools.push(ev as AgentToolEvent);
            updateLast((m) => ({ ...m, tools: [...tools] }));
          } else if (ev.type === "error") {
            err(ev.error || "agent error");
          }
        }
      } else {
        for await (const delta of chatStream(key, model, history)) {
          acc += delta;
          updateLast((m) => ({ ...m, content: acc }));
        }
      }
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setStreaming(false);
      // Save once the turn completes (assistant message settled).
      const done = next.concat([{ role: "assistant", content: acc, tools }]);
      persist(done);
    }
  };

  return (
    <>
      <div className="card">
        <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Conversations</h2>
          <button className="btn sm primary" onClick={newChat} disabled={streaming}>
            + New chat
          </button>
        </div>
        {chats.length === 0 ? (
          <div className="empty muted" style={{ padding: "10px 0" }}>no saved conversations yet</div>
        ) : (
          <table>
            <tbody>
              {chats.map((c) => (
                <tr key={c.id} style={c.id === convId ? { background: "rgba(128,128,128,0.08)" } : undefined}>
                  <td>
                    <a
                      href="#"
                      onClick={(e) => {
                        e.preventDefault();
                        openChat(c.id);
                      }}
                      style={{ textDecoration: "none" }}
                    >
                      {c.title || "Untitled"}
                    </a>{" "}
                    {c.agentMode && <span className="badge">agent</span>}
                    <div className="muted" style={{ fontSize: 12 }}>
                      {c.model} · {c.messages} msgs · {new Date(c.updatedAt).toLocaleString()}
                    </div>
                  </td>
                  <td style={{ textAlign: "right", verticalAlign: "middle" }}>
                    <button className="btn sm danger" disabled={streaming} onClick={() => removeChat(c.id)}>
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <div className="card">
        <div className="row">
          <div>
            <label>Model (running instance)</label>
            <select value={model} onChange={(e) => setModel(e.target.value)}>
              <option value="">— none running —</option>
              {running.map((i) => (
                <option key={i.id} value={i.modelId}>
                  {i.modelId}
                </option>
              ))}
            </select>
          </div>
          <div style={{ flex: 1 }}>
            <label>API key</label>
            <div className="row">
              <input
                type="password"
                style={{ flex: 1, minWidth: 160 }}
                className="mono"
                value={key}
                onChange={(e) => setKey(e.target.value)}
                placeholder="paste your full key"
              />
              <button className="btn sm" onClick={() => setShowKey((s) => !s)}>
                {showKey ? "Hide" : "Show"}
              </button>
            </div>
          </div>
          <div style={{ alignSelf: "flex-end" }}>
            <label style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
              <input type="checkbox" checked={agentMode} onChange={(e) => setAgentMode(e.target.checked)} />
              Agent (web search)
            </label>
          </div>
        </div>
        {agentMode && (
          <p className="muted small" style={{ marginTop: 8 }}>
            Agent mode: the model can search the web for current information; searches and sources appear inline.
          </p>
        )}
        {showKey && (
          <p className="muted small" style={{ marginTop: 8 }}>
            The key is stored only in this browser (localStorage). Create keys on the API Keys page.
          </p>
        )}
        {!running.length && (
          <p className="muted small" style={{ marginTop: 8 }}>
            No running instances. Start one on the Instances page to chat.
          </p>
        )}
      </div>
      <div className="card">
        <div className="chat">
          {msgs.length === 0 && (
            <div className="empty">select a running model and start chatting</div>
          )}
          {msgs.map((m, i) => (
            <div key={i} className={`msg ${m.role}`}>
              <div className="who">{m.role}</div>
              {m.tools && m.tools.length > 0 && (
                <div style={{ marginBottom: 6 }}>
                  {m.tools.map((t, j) => (
                    <details key={j} className="muted" style={{ fontSize: 12, marginBottom: 4 }}>
                      <summary style={{ cursor: "pointer" }}>
                        🔍 {t.name && t.name !== "web_search" ? `${t.name}:` : "searched:"} {t.query || "(no query)"}
                        {t.error ? " (error)" : ` — ${t.results?.length ?? 0} results`}
                      </summary>
                      <ul style={{ margin: "4px 0 4px 18px", padding: 0 }}>
                        {(t.results || []).map((r, k) => (
                          <li key={k} style={{ listStyle: "none", marginBottom: 3 }}>
                            <a href={r.url} target="_blank" rel="noreferrer">
                              {r.title}
                            </a>
                            {r.snippet && <div style={{ opacity: 0.75 }}>{r.snippet.slice(0, 160)}</div>}
                          </li>
                        ))}
                      </ul>
                    </details>
                  ))}
                </div>
              )}
              {m.content || (streaming && i === msgs.length - 1 ? "…" : "")}
            </div>
          ))}
          <div ref={bottomRef} />
        </div>
        <div className="row" style={{ marginTop: 12 }}>
          <textarea
            rows={2}
            style={{ flex: 1, minWidth: 200 }}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                send();
              }
            }}
            placeholder={
              agentMode
                ? "Ask anything — the agent can search the web (Enter to send)"
                : "Type a message (Enter to send, Shift+Enter for newline)"
            }
          />
          <div style={{ alignSelf: "flex-end" }}>
            <button className="btn primary" disabled={streaming || !input.trim()} onClick={send}>
              {streaming ? "…" : "Send"}
            </button>
          </div>
        </div>
      </div>
    </>
  );
}
