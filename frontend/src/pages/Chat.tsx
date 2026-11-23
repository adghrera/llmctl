import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { agentStream, chatStream, api, type AgentToolEvent, type ChatSummary, type SearchChatResult, type SearchMatch, type KB } from "../api/client";
import { useStore } from "../state/store";
import { err, ok } from "../ui/toast";
import { renderMd } from "../md";
import { ago } from "../lib/time";

// highlightFind wraps query matches in <mark> inside rendered markdown HTML,
// touching only text nodes (never tag markup) — find-in-conversation (F812).
function highlightFind(html: string, query: string): string {
  const q = query.trim();
  if (!q) return html;
  const esc = q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return html.replace(/>([^<]+)</g, (_all, text: string) => ">" + text.replace(new RegExp(esc, "gi"), "<mark>$&</mark>") + "<");
}

interface Msg {
  role: "user" | "assistant";
  content: string;
  /** agent steps recorded while producing this message (searches run) */
  tools?: AgentToolEvent[];
  /** real token usage from the upstream usage chunk (F078) */
  tokens?: { prompt?: number; completion?: number; total?: number };
}

const KEY_STORAGE = "llmctl.chat.key";
const AGENT_STORAGE = "llmctl.chat.agent";
const AGENT_TOOLS_STORAGE = "llmctl.chat.agenttools"; // per-chat tool toggles (F820)
const ALL_AGENT_TOOLS = ["web_search", "fetch_url", "calculator", "current_time"];
const APPROVAL_STORAGE = "llmctl.chat.approval";

export default function Chat() {
  const { instances } = useStore();
  const running = instances.filter((i) => i.status === "running");
  // `?model=` deep-link from the Instances page preselects the model (F362).
  const [params, setParams] = useSearchParams();
  const [model, setModel] = useState(() => params.get("model") ?? "");
  const [key, setKey] = useState(() => localStorage.getItem(KEY_STORAGE) ?? "");
  const [agentMode, setAgentMode] = useState(() => localStorage.getItem(AGENT_STORAGE) === "1");
  const [agentTools, setAgentTools] = useState<Set<string>>(() => {
    const saved = localStorage.getItem(AGENT_TOOLS_STORAGE);
    return new Set(saved === null ? ALL_AGENT_TOOLS : saved.split(",").filter(Boolean));
  }); // built-in agent tools enabled (F820)
  const [requireApproval, setRequireApproval] = useState(
    () => localStorage.getItem(APPROVAL_STORAGE) === "1"
  );
  const [pendingApproval, setPendingApproval] = useState<{ id: string; name: string; query: string } | null>(null);
  const [systemPrompt, setSystemPrompt] = useState(() => localStorage.getItem("llmctl.chat.system") ?? "");
  const [maxTok, setMaxTok] = useState(() => localStorage.getItem("llmctl.chat.maxtok") ?? "");
  const [temp, setTemp] = useState(() => localStorage.getItem("llmctl.chat.temp") ?? "");
  const [seed, setSeed] = useState(() => localStorage.getItem("llmctl.chat.seed") ?? "");
  const [topP, setTopP] = useState(() => localStorage.getItem("llmctl.chat.topp") ?? "");
  const [freqPen, setFreqPen] = useState(() => localStorage.getItem("llmctl.chat.freqpen") ?? "");
  const [presPen, setPresPen] = useState(() => localStorage.getItem("llmctl.chat.prespen") ?? "");
  const [find, setFind] = useState(""); // find-in-conversation query (F812)
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const abortRef = useRef<AbortController | null>(null); // stop in-flight stream (F754)
  const [showKey, setShowKey] = useState(!key);
  const bottomRef = useRef<HTMLDivElement>(null);

  // ---- knowledge bases (F047) ----
  const [kbs, setKBs] = useState<KB[]>([]);
  const [selectedKBs, setSelectedKBs] = useState<Set<string>>(new Set());

  // ---- saved conversations (F025) ----
  const [convId, setConvId] = useState<string | null>(null);
  const [chats, setChats] = useState<ChatSummary[]>([]);
  const [context, setContext] = useState<{ usedTokens: number; limitTokens: number; pct: number } | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<SearchChatResult[]>([]);
  const [searching, setSearching] = useState(false);
  const [forkDialog, setForkDialog] = useState<{
    id: string | null;
    title: string;
    messages: number;
  } | null>(null);
  const [forkAt, setForkAt] = useState(0);
  const [forkTitle, setForkTitle] = useState("");
  const convIdRef = useRef<string | null>(null);
  convIdRef.current = convId;

  const loadChats = () => api.chats().then(setChats).catch(() => setChats([]));
  const [showStats, setShowStats] = useState(false); // per-model rollup (CLI `chats -stats` parity, F633)
  const [tagFilter, setTagFilter] = useState(""); // sidebar tag filter (F678)
  const chatTags = Array.from(new Set(chats.flatMap((c) => c.tags ?? []))).sort();
  const visibleChats = tagFilter ? chats.filter((c) => (c.tags ?? []).includes(tagFilter)) : chats;
  const loadKBs = () => api.knowledgeBases().then(setKBs).catch(() => setKBs([]));
  useEffect(() => {
    loadChats();
    loadKBs();
    // Prompt page "Insert into chat" hands the template over this way (F300).
    const seed = sessionStorage.getItem("llmctl.chatSeed");
    if (seed) {
      sessionStorage.removeItem("llmctl.chatSeed");
      setInput(seed);
    }
  }, []);

  const searchConversations = async (q: string) => {
    if (!q.trim()) {
      setSearchResults([]);
      setSearchQuery("");
      return;
    }
    setSearchQuery(q);
    setSearching(true);
    try {
      const results = await api.searchChats(q);
      setSearchResults(results);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
      setSearchResults([]);
    } finally {
      setSearching(false);
    }
  };

  // Persist current messages: create on first save, then update in place.
  const persist = async (messages: Msg[]) => {
    if (!messages.length) return;
    const payload = {
      model,
      agentMode,
      system: systemPrompt.trim() || undefined,
      messages: messages.map((m) => ({
        role: m.role,
        content: m.content,
        tools: (m.tools || []).map((t) => ({ name: t.name, query: t.query, results: t.results })),
        tokens: m.tokens,
      })),
    };
    try {
      if (convIdRef.current) {
        await api.saveChat(convIdRef.current, payload);
      } else {
        const created = await api.createChat(payload);
        setConvId(created.id);
        setParams({ c: created.id }, { replace: true }); // deep-link follows the new chat (F813)
      }
      await loadChats();
      // Refresh context usage from the server after saving.
      if (convIdRef.current) {
        try {
          const c = await api.getChat(convIdRef.current);
          if (c.context) setContext(c.context);
        } catch { /* context refresh is best-effort */ }
      }
    } catch (e) {
      err(`could not save conversation: ${e instanceof Error ? e.message : e}`);
    }
  };

  const newChat = () => {
    if (streaming) return;
    setMsgs([]);
    setConvId(null);
    setInput("");
    setSystemPrompt("");
    setParams({}, { replace: true }); // deep-link cleared (F813)
  };

  // Import an exported chat JSON file as a new conversation (F369).
  const importFileRef = useRef<HTMLInputElement>(null);
  const importChat = async (file: File) => {
    try {
      const parsed = JSON.parse(await file.text());
      if (!Array.isArray(parsed.messages) || parsed.messages.length === 0) {
        err("file has no messages — expected a chats export JSON");
        return;
      }
      delete parsed.id; // fresh id server-side
      delete parsed.context;
      const saved = await api.importChat(parsed);
      await loadChats();
      ok(`imported "${saved.title || saved.id}"`);
      openChat(saved.id);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const openChat = async (id: string) => {
    if (streaming) return;
    try {
      const c = await api.getChat(id);
      setMsgs(
        (c.messages || []).map((m) => ({
          role: m.role,
          content: m.content,
          tokens: m.tokens,
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
      setParams({ c: c.id }, { replace: true }); // deep-link follows the open chat (F813)
      if (c.model) setModel(c.model);
      if (typeof c.agentMode === "boolean") setAgentMode(c.agentMode);
      setSystemPrompt(c.system || "");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  // Deep link: /chat?c=<id> opens that conversation on load (F813).
  const deepLinked = useRef(false);
  useEffect(() => {
    if (deepLinked.current) return;
    deepLinked.current = true;
    const c = params.get("c");
    if (c) openChat(c);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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

  // Bulk-delete old unpinned chats — UI twin of `llmctl chats prune` (F660).
  const pruneOldChats = async () => {
    const input = window.prompt("Delete chats last updated more than N days ago (pinned kept):", "30");
    if (input === null) return;
    const days = parseInt(input, 10);
    if (!days || days < 1) {
      err("enter a positive number of days");
      return;
    }
    const cutoff = Date.now() - days * 86400000;
    const doomed = chats.filter((c) => !c.pinned && c.updatedAt && new Date(c.updatedAt).getTime() < cutoff);
    if (doomed.length === 0) {
      ok(`no chats older than ${days} days`);
      return;
    }
    if (!window.confirm(`Delete ${doomed.length} chat(s) older than ${days} days?`)) return;
    let deleted = 0;
    for (const c of doomed) {
      try {
        await api.deleteChat(c.id);
        deleted++;
      } catch { /* best-effort, like the CLI */ }
    }
    if (convId && doomed.some((c) => c.id === convId)) newChat();
    ok(`pruned ${deleted} chat(s)`);
    await loadChats();
  };

  const renameChat = async (id: string, current: string) => {
    const t = window.prompt("New title", current);
    if (t == null || !t.trim()) return;
    try {
      const c = await api.getChat(id);
      await api.saveChat(id, { ...c, title: t.trim() });
      ok("renamed");
      await loadChats();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const togglePin = async (id: string, pinned: boolean) => {
    try {
      await api.pinChat(id, !pinned);
      await loadChats();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  // free-form tags, comma-separated in the prompt (F676); empty clears.
  const tagChat = async (id: string, current: string[]) => {
    const next = window.prompt(`Tags for this chat (comma-separated, empty clears):`, current.join(", "));
    if (next == null) return;
    const tags = next.split(",").map((t) => t.trim()).filter(Boolean);
    try {
      await api.setChatTags(id, tags);
      ok(tags.length ? `tags set: ${tags.join(", ")}` : "tags cleared");
      await loadChats();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const forkChat = async (id: string, forkAt: number, title: string) => {
    try {
      const c = await api.forkChat(id, { forkAt, title });
      ok(`forked as "${c.title}"`);
      setForkDialog(null);
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
    localStorage.setItem(APPROVAL_STORAGE, requireApproval ? "1" : "0");
  }, [requireApproval]);
  const toggleAgentTool = (t: string) => {
    setAgentTools((prev) => {
      const next = new Set(prev);
      if (next.has(t)) next.delete(t);
      else next.add(t);
      localStorage.setItem(AGENT_TOOLS_STORAGE, Array.from(next).join(","));
      return next;
    });
  };

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [msgs]);

  // Default to the first running instance; also fall back if a deep-linked
  // or selected model stops running (F362).
  useEffect(() => {
    if (running.length && !running.some((i) => i.modelId === model)) {
      setModel(running[0].modelId);
    }
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
    if (!model) {
      err("no running instance — start one on the Instances page");
      return;
    }

    // Check for skill invocation: /s.<short_name> <rest>
    const skillMatch = text.match(/^\/s\.([a-z0-9][a-z0-9_-]*)\s+(.*)$/s);
    let skillMsg: Msg | null = null;
    let userMsg: Msg = { role: "user", content: text };

    if (skillMatch) {
      const shortName = skillMatch[1];
      const rest = skillMatch[2];
      try {
        const skill = await api.getSkill(shortName);
        if (!skill.enabled) {
          err(`skill "${shortName}" is disabled`);
          return;
        }
        // Format prior conversation history for {{conversation}}
        const historyForSkill = msgs
          .slice(-10) // last 10 messages for context
          .map((m) => `${m.role.toUpperCase()}: ${m.content}`)
          .join("\n");
        const rendered = skill.manifest.prompt_template
          .split("{{input}}").join(rest)
          .split("{{conversation}}").join(historyForSkill || "(none)");
        skillMsg = { role: "user", content: rendered };
        userMsg = { role: "user", content: rest };
      } catch (e) {
        err(e instanceof Error ? e.message : `skill "${shortName}" not found`);
        return;
      }
    }

    // Check for file reference: /file <filename>
    let fileMsg: Msg | null = null;
    const fileMatch = text.match(/^\/file\s+(.+)$/);
    if (fileMatch) {
      const fileName = fileMatch[1].trim();
      try {
        const files = await api.files();
        const f = files.find((f) => f.entry.filename === fileName);
        if (!f) {
          err(`file "${fileName}" not found`);
          return;
        }
        if (!isTextFile(f.entry.contentType, f.entry.filename)) {
          err(`file "${fileName}" is not text-based`);
          return;
        }
        const content = await api.getFileContent(f.id);
        fileMsg = { role: "user", content: `File: ${f.entry.filename}\n\n${content}` };
        userMsg = { role: "user", content: text.replace(fileMatch[0], "").trim() || "Please analyze this file." };
      } catch (e) {
        err(e instanceof Error ? e.message : `failed to load file "${fileName}"`);
        return;
      }
    }

    // KB context retrieval via vector search; multiple KBs are searched and
    // the merged top hits are used (F666).
    let kbMsg: Msg | null = null;
    if (selectedKBs.size > 0 && text.trim()) {
      try {
        const per = await Promise.all(
          kbs
            .filter((k) => selectedKBs.has(k.id))
            .map(async (k) => {
              const rs = await api.searchKBDocs(k.id, { q: text, limit: 3 });
              return rs.map((r) => ({ ...r, kbName: k.kb.name }));
            })
        );
        const merged = per
          .flat()
          .sort((a, b) => b.score - a.score)
          .slice(0, 3);
        if (merged.length > 0) {
          const names = [...new Set(merged.map((r) => r.kbName))].join(", ");
          const kbContext = merged
            .map((r) => `Document: ${r.filename} (relevance: ${(r.score * 100).toFixed(1)}%)\n${r.chunk_text}`)
            .join("\n\n---\n\n");
          kbMsg = {
            role: "user",
            content: `Relevant context from knowledge base "${names}":\n\n${kbContext}`,
          };
        }
      } catch (e) {
        // KB search failure is non-fatal; continue without KB context
      }
    }

    const next: Msg[] = skillMsg
      ? [...msgs, skillMsg, fileMsg, kbMsg, userMsg].filter((m): m is Msg => m != null)
      : [...msgs, fileMsg, kbMsg, userMsg].filter((m): m is Msg => m != null);
    setInput("");
    await runStream(next);
  };

  // runStream streams one assistant turn over the given message list and
  // persists the result. Shared by send() and regenerate() (F064).
  const runStream = async (next: Msg[]) => {
    setMsgs(next);
    setStreaming(true);
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    const history: { role: string; content: string }[] = next.map((m) => ({ role: m.role, content: m.content }));
    if (systemPrompt.trim()) {
      history.unshift({ role: "system", content: systemPrompt.trim() });
    }
    let acc = "";
    const tools: AgentToolEvent[] = [];
    let usage: Msg["tokens"] | undefined;
    try {
      // Seed a single assistant message; each delta updates it in place.
      setMsgs((prev) => [...prev, { role: "assistant", content: "", tools: [] }]);
      if (agentMode) {
        for await (const ev of agentStream(key, {
          model,
          messages: history,
          requireApproval,
          tools: Array.from(agentTools), // per-chat tool toggles (F820)
        }, { signal: ctrl.signal })) {
          if (ev.type === "delta" && ev.content) {
            acc += ev.content;
            updateLast((m) => ({ ...m, content: acc }));
          } else if (ev.type === "tool") {
            tools.push(ev as AgentToolEvent);
            // Any text accumulated before a tool event is pre-marker
            // noise from the tool-call turn (possibly a truncated marker
            // fragment). The real answer streams after tool results, so
            // drop the stale prefix.
            acc = "";
            updateLast((m) => ({ ...m, content: acc, tools: [...tools] }));
          } else if (ev.type === "approval" && ev.id) {
            setPendingApproval({ id: ev.id, name: ev.name || "tool", query: ev.query || "{}" });
          } else if (ev.type === "error") {
            err(ev.error || "agent error");
          } else if (ev.type === "system") {
            err(ev.content || "agent notice");
          }
        }
      } else {
        for await (const delta of chatStream(key, model, history, {
          maxTokens: Number(maxTok) > 0 ? Number(maxTok) : undefined,
          temperature: temp.trim() !== "" ? Number(temp) : undefined,
          seed: seed.trim() !== "" ? Number(seed) : undefined,
          topP: topP.trim() !== "" ? Number(topP) : undefined,
          freqPenalty: freqPen.trim() !== "" ? Number(freqPen) : undefined,
          presPenalty: presPen.trim() !== "" ? Number(presPen) : undefined,
          signal: ctrl.signal,
          onUsage: (u) => {
            usage = u;
          },
        })) {
          acc += delta;
          updateLast((m) => ({ ...m, content: acc }));
        }
      }
    } catch (e) {
      if (e instanceof DOMException && e.name === "AbortError") ok("stopped");
      else err(e instanceof Error ? e.message : String(e));
    } finally {
      abortRef.current = null;
      setStreaming(false);
      // Save once the turn completes (assistant message settled).
      const done = next.concat([{ role: "assistant", content: acc, tools, tokens: usage }]);
      persist(done);
    }
  };

  // Regenerate: drop the trailing assistant reply and re-run the last turn (F064).
  const regenerate = async () => {
    if (streaming || !msgs.length) return;
    const base = [...msgs];
    if (base[base.length - 1]?.role === "assistant") base.pop();
    if (!base.length || base[base.length - 1].role !== "user") {
      err("nothing to regenerate");
      return;
    }
    await runStream(base);
  };

  // Edit: pull the last user message back into the input box and drop it
  // (plus the reply) from the conversation (F064).
  const editLastUser = () => {
    if (streaming || !msgs.length) return;
    const base = [...msgs];
    if (base[base.length - 1]?.role === "assistant") base.pop();
    if (!base.length || base[base.length - 1].role !== "user") return;
    const last = base.pop() as Msg;
    setMsgs(base);
    setInput(last.content);
    if (base.length) persist(base);
  };

  const answerApproval = async (approve: boolean) => {    if (!pendingApproval) return;
    const { id } = pendingApproval;
    setPendingApproval(null);
    try {
      await api.agentApprove(id, approve);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const lastUserIdx = (() => {
    for (let j = msgs.length - 1; j >= 0; j--) if (msgs[j].role === "user") return j;
    return -1;
  })();

  return (
    <>
      <div className="card">
        <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Conversations</h2>
          <div className="row" style={{ gap: 6 }}>
            <button
              className="btn sm"
              aria-pressed={showStats}
              disabled={chats.length === 0}
              title="group conversations per model — CLI: llmctl chats -stats (F633)"
              onClick={() => setShowStats(!showStats)}
            >
              {showStats ? "Show list" : "Stats"}
            </button>
            <button
              className="btn sm"
              disabled={streaming || chats.length === 0}
              title="download the conversation list as CSV (CLI: llmctl chats list --csv)"
              onClick={() => {
                // Same columns as `llmctl chats list --csv` (F392).
                const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                const rows = [
                  "id,title,model,messages,tokens,updated_at,tags",
                  ...chats.map((c) =>
                    [c.id, c.title, c.model || "", String(c.messages ?? 0), String(c.tokens ?? 0), c.updatedAt || "", (c.tags ?? []).join(";")]
                      .map(esc)
                      .join(",")
                  ),
                ];
                const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-chats-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
            <button
              className="btn sm"
              disabled={streaming || chats.length === 0}
              title="download the conversation list as JSON (CLI: llmctl chats list -json)"
              onClick={() => {
                const blob = new Blob([JSON.stringify(chats, null, 2)], { type: "application/json" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-chats-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export JSON
            </button>
            <button
              className="btn sm"
              disabled={streaming}
              title="import a chat exported as JSON (CLI: llmctl chats import)"
              onClick={() => importFileRef.current?.click()}
            >
              Import
            </button>
            <button
              className="btn sm"
              disabled={streaming || chats.length === 0}
              title="delete chats older than N days, pinned kept (CLI: llmctl chats prune)"
              onClick={pruneOldChats}
            >
              Prune
            </button>
            <input
              ref={importFileRef}
              type="file"
              accept="application/json,.json"
              style={{ display: "none" }}
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) importChat(f);
                e.target.value = "";
              }}
            />
            <button className="btn sm primary" onClick={newChat} disabled={streaming}>
              + New chat
            </button>
          </div>
        </div>
        <div className="row" style={{ marginTop: 8 }}>
          <input
            style={{ flex: 1, minWidth: 200 }}
            placeholder="Search conversations…"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && searchConversations(searchQuery)}
          />
          <button
            className="btn sm"
            disabled={searching || !searchQuery.trim()}
            onClick={() => searchConversations(searchQuery)}
          >
            {searching ? "Searching…" : "Search"}
          </button>
          <button
            className="btn sm"
            disabled={!searchQuery.trim()}
            onClick={() => {
              setSearchQuery("");
              setSearchResults([]);
            }}
          >
            Clear
          </button>
        </div>
        {searchQuery.trim() && searchResults.length === 0 && !searching && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No conversations matched "{searchQuery}"
          </div>
        )}
        {searchQuery.trim() && searching && (
          <div className="empty muted" style={{ padding: "10px 0" }}>Searching…</div>
        )}
        {searchQuery.trim() && searchResults.length > 0 && (
          <div style={{ marginTop: 8 }}>
            <p style={{ fontSize: 12, opacity: 0.7, marginBottom: 6 }}>
              Search results for "{searchQuery}" ({searchResults.length})
            </p>
            <table>
              <tbody>
                {searchResults.map((r) => (
                  <tr key={r.conversationId} style={{ cursor: "pointer" }} onClick={() => openChat(r.conversationId)}>
                    <td style={{ width: "60%" }}>
                      <b>{r.title}</b>
                      <div style={{ fontSize: 12, opacity: 0.7 }}>{r.model || "—"} • <span title={new Date(r.updatedAt).toLocaleString()}>{ago(r.updatedAt)}</span></div>
                    </td>
                    <td>
                      {r.matches.map((m: SearchMatch, i: number) => (
                        <div key={i} style={{ fontSize: 11, opacity: 0.85, marginBottom: 4 }}>
                          <span style={{ textTransform: "uppercase", fontSize: 9, opacity: 0.6 }}>{m.role}</span>{" "}
                          {m.content}
                        </div>
                      ))}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {!searchQuery.trim() && chats.length === 0 && (
          <div className="empty muted" style={{ padding: "10px 0" }}>no saved conversations yet</div>
        )}
        {!searchQuery.trim() && chats.length > 0 && showStats && (
          <table>
            <thead>
              <tr>
                <th>Model</th>
                <th>Chats</th>
                <th>Msgs</th>
                <th>Tokens</th>
                <th>Last</th>
              </tr>
            </thead>
            <tbody>
              {(() => {
                // Same rollup as `llmctl chats -stats` (F633): one row per
                // model, sorted by tokens.
                const by = new Map<string, { chats: number; msgs: number; tokens: number; last: string }>();
                for (const c of chats) {
                  const m = c.model || "(none)";
                  const g = by.get(m) ?? { chats: 0, msgs: 0, tokens: 0, last: "" };
                  g.chats++;
                  g.msgs += c.messages ?? 0;
                  g.tokens += c.tokens ?? 0;
                  if ((c.updatedAt || "") > g.last) g.last = c.updatedAt || "";
                  by.set(m, g);
                }
                return [...by.entries()]
                  .sort((a, b) => b[1].tokens - a[1].tokens)
                  .map(([model, g]) => (
                    <tr key={model}>
                      <td>{model}</td>
                      <td>{g.chats}</td>
                      <td>{g.msgs}</td>
                      <td>{g.tokens}</td>
                      <td className="muted" style={{ fontSize: 12 }}>
                        {g.last ? new Date(g.last).toLocaleString() : ""}
                      </td>
                    </tr>
                  ));
              })()}
            </tbody>
          </table>
        )}
        {!searchQuery.trim() && chats.length > 0 && !showStats && (
          <>
          {chatTags.length > 0 && (
            <div className="row" style={{ gap: 6, marginBottom: 6, alignItems: "center" }}>
              <select
                className="input"
                style={{ fontSize: 12 }}
                title="filter the list by tag (CLI: llmctl chats list -tag T, F678)"
                value={tagFilter}
                onChange={(e) => setTagFilter(e.target.value)}
              >
                <option value="">all tags</option>
                {chatTags.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
              {tagFilter && (
                <span className="muted" style={{ fontSize: 12 }}>
                  {visibleChats.length} of {chats.length}
                </span>
              )}
            </div>
          )}
          <table>
            <tbody>
              {visibleChats.map((c) => (
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
                    {c.pinned && <span title="pinned">📌</span>}
                    {c.agentMode && <span className="badge">agent</span>}
                    <div className="muted" style={{ fontSize: 12 }}>
                      {c.model} · {c.messages} msgs · {c.tokens ?? 0} tok · <span title={new Date(c.updatedAt).toLocaleString()}>{ago(c.updatedAt)}</span>
                      {c.tags?.length ? <span title={c.tags.join(", ")}> · {(c.tags || []).join(", ")}</span> : null}
                    </div>
                  </td>
                  <td style={{ textAlign: "right", verticalAlign: "middle" }}>
                    <button className="btn sm" disabled={streaming} onClick={() => togglePin(c.id, !!c.pinned)}>
                      {c.pinned ? "Unpin" : "Pin"}
                    </button>{" "}
                    <button className="btn sm" disabled={streaming} onClick={() => renameChat(c.id, c.title || "")}>
                      Rename
                    </button>{" "}
                    <button className="btn sm" disabled={streaming} onClick={() => tagChat(c.id, c.tags ?? [])}>
                      Tag
                    </button>{" "}
                    <button className="btn sm danger" disabled={streaming} onClick={() => removeChat(c.id)}>
                      Delete
                    </button>{" "}
                    <a
                      href={`/api/v1/chats/${encodeURIComponent(c.id)}/export?format=json`}
                      download={c.id + ".json"}
                    >
                      <button className="btn sm" disabled={streaming}>
                        Export JSON
                      </button>
                    </a>{" "}
                    <a
                      href={`/api/v1/chats/${encodeURIComponent(c.id)}/export?format=markdown`}
                      download={c.title + ".md"}
                    >
                      <button className="btn sm" disabled={streaming}>
                        Export MD
                      </button>
                    </a>{" "}
                    <button
                      className="btn sm"
                      disabled={streaming || c.messages === 0}
                      onClick={() => {
                        setForkAt(0);
                        setForkTitle((c.title || "Untitled") + " (fork)");
                        setForkDialog({ id: c.id, title: c.title || "Untitled", messages: c.messages });
                      }}
                    >
                      Fork
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          </>
        )}
        {/* The low-fidelity F042 import section is replaced by the full-fidelity
            "Import" button in the card header (F369). */}
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
            </label>{" "}
            {agentMode && (
              <span style={{ fontSize: 12 }} title="which built-in tools the agent may use (F820)">
                {[
                  ["web_search", "search"],
                  ["fetch_url", "fetch"],
                  ["calculator", "calc"],
                  ["current_time", "time"],
                ].map(([t, label]) => (
                  <label key={t} style={{ marginRight: 8 }}>
                    <input type="checkbox" checked={agentTools.has(t)} onChange={() => toggleAgentTool(t)} />{" "}
                    {label}
                  </label>
                ))}
              </span>
            )}{" "}
            <label
              style={{ display: "inline-flex", gap: 6, alignItems: "center", marginLeft: 12 }}
              title="The agent asks for confirmation before running any tool"
            >
              <input
                type="checkbox"
                checked={requireApproval}
                onChange={(e) => setRequireApproval(e.target.checked)}
                disabled={!agentMode}
              />
              Ask before tool calls
            </label>
          </div>
          <div style={{ alignSelf: "flex-end", marginTop: 8 }}>
            {kbs.length > 0 ? (
              <label style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12 }}>
                <span className="muted">
                  Knowledge base context{selectedKBs.size > 0 ? ` (${selectedKBs.size} selected)` : ""}
                </span>
                <select
                  multiple
                  size={Math.min(4, kbs.length)}
                  value={[...selectedKBs]}
                  onChange={(e) => setSelectedKBs(new Set(Array.from(e.target.selectedOptions).map((o) => o.value)))}
                >
                  {kbs.map((kb) => (
                    <option key={kb.id} value={kb.id}>
                      {kb.kb.name}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <span style={{ opacity: 0.6, fontSize: 12 }}>no knowledge bases</span>
            )}
          </div>
          <details style={{ alignSelf: "stretch", marginTop: 8 }}>
            <summary className="muted" style={{ cursor: "pointer", fontSize: 13 }}>
              System prompt{systemPrompt.trim() ? " (set)" : ""} · sampling
            </summary>
            <textarea
              rows={3}
              style={{ width: "100%", marginTop: 6, fontSize: 12 }}
              placeholder="Optional per-conversation system prompt, sent with every request"
              value={systemPrompt}
              onChange={(e) => {
                setSystemPrompt(e.target.value);
                // Persist like the Compare page system prompt (F762).
                localStorage.setItem("llmctl.chat.system", e.target.value);
              }}
            />
            <div className="row" style={{ gap: 8, marginTop: 6, flexWrap: "wrap" }}>
              <input
                style={{ width: 130 }}
                type="number"
                min={1}
                placeholder="max tokens"
                title="max tokens per reply (empty = model default)"
                value={maxTok}
                onChange={(e) => {
                  setMaxTok(e.target.value);
                  localStorage.setItem("llmctl.chat.maxtok", e.target.value);
                }}
              />
              <input
                style={{ width: 130 }}
                type="number"
                step="0.1"
                min={0}
                max={2}
                placeholder="temperature"
                title="sampling temperature (empty = model default)"
                value={temp}
                onChange={(e) => {
                  setTemp(e.target.value);
                  localStorage.setItem("llmctl.chat.temp", e.target.value);
                }}
              />
              <input
                style={{ width: 110 }}
                type="number"
                placeholder="seed"
                title="sampling seed for reproducible replies, sent through to the backend (empty = random) (F793)"
                value={seed}
                onChange={(e) => {
                  setSeed(e.target.value);
                  localStorage.setItem("llmctl.chat.seed", e.target.value);
                }}
              />
              <input
                style={{ width: 90 }}
                type="number"
                step="0.05"
                min={0}
                max={1}
                placeholder="top_p"
                title="nucleus sampling top_p, sent through to the backend (empty = model default) (F797)"
                value={topP}
                onChange={(e) => {
                  setTopP(e.target.value);
                  localStorage.setItem("llmctl.chat.topp", e.target.value);
                }}
              />
              <input
                style={{ width: 100 }}
                type="number"
                step="0.1"
                min={-2}
                max={2}
                placeholder="freq penalty"
                title="frequency penalty, sent through to the backend (empty = model default) (F803)"
                value={freqPen}
                onChange={(e) => {
                  setFreqPen(e.target.value);
                  localStorage.setItem("llmctl.chat.freqpen", e.target.value);
                }}
              />
              <input
                style={{ width: 100 }}
                type="number"
                step="0.1"
                min={-2}
                max={2}
                placeholder="pres penalty"
                title="presence penalty, sent through to the backend (empty = model default) (F803)"
                value={presPen}
                onChange={(e) => {
                  setPresPen(e.target.value);
                  localStorage.setItem("llmctl.chat.prespen", e.target.value);
                }}
              />
              <button
                className="btn sm"
                title="copy a curl command for this conversation's next request"
                onClick={() => {
                  // Same body the stream sends (model, messages, stream, sampling).
                  const history: { role: string; content: string }[] = msgs.map((m) => ({ role: m.role, content: m.content }));
                  if (systemPrompt.trim()) history.unshift({ role: "system", content: systemPrompt.trim() });
                  const body: Record<string, unknown> = { model, messages: history, stream: true };
                  if (maxTok) body.max_tokens = Number(maxTok);
                  if (temp) body.temperature = Number(temp);
                  if (seed.trim() !== "") body.seed = Number(seed);
                  if (topP.trim() !== "") body.top_p = Number(topP);
                  if (freqPen.trim() !== "") body.frequency_penalty = Number(freqPen);
                  if (presPen.trim() !== "") body.presence_penalty = Number(presPen);
                  const json = JSON.stringify(body);
                  const q = `'${json.replace(/'/g, `'\\''`)}'`; // shell-safe single quoting
                  const cmd =
                    `curl -N ${window.location.origin}/v1/chat/completions \\\n` +
                    `  -H "Authorization: Bearer $LLMCTL_API_KEY" \\\n` +
                    `  -H "Content-Type: application/json" \\\n` +
                    `  -d ${q}\n`;
                  navigator.clipboard
                    .writeText(cmd)
                    .then(() => ok("copied curl command"))
                    .catch(() => err("clipboard unavailable"));
                }}
              >
                Copy cURL
              </button>
            </div>
          </details>
        </div>
        {agentMode && (
          <p className="muted small" style={{ marginTop: 8 }}>
            Agent mode: the model can search the web for current information; searches and sources appear inline.
          </p>
        )}
        {showKey && (
          <p className="muted small" style={{ marginTop: 8 }}>
            API key is optional unless gateway auth is enabled (Settings → Require API Key).
            The key is stored only in this browser (localStorage).
          </p>
        )}
        {!running.length && (
          <p className="muted small" style={{ marginTop: 8 }}>
            No running instances. Start one on the Instances page to chat.
          </p>
        )}
      </div>
      {context && (
        <div className="card">
          <div className="row" style={{ alignItems: "center", gap: 10 }}>
            <span style={{ fontSize: 12, opacity: 0.8 }}>Context window</span>
            <div
              style={{
                flex: 1,
                height: 6,
                background: "rgba(127,127,127,0.2)",
                borderRadius: 3,
                overflow: "hidden",
              }}
            >
              <div
                style={{
                  height: "100%",
                  width: Math.min(context.pct, 100) + "%",
                  background:
                    context.pct >= 98
                      ? "#c44"
                      : context.pct >= 80
                        ? "#c90"
                        : "#3a6",
                  borderRadius: 3,
                  transition: "width 0.2s",
                }}
              />
            </div>
            <span style={{ fontSize: 12, minWidth: 50, textAlign: "right" }}>
              {context.usedTokens} / {context.limitTokens || "?"} tokens
              {context.limitTokens > 0 ? ` (${context.pct.toFixed(0)}%)` : ""}
            </span>
            {context.pct >= 98 && (
              <span style={{ fontSize: 12, color: "#c44" }}>(auto-compact soon)</span>
            )}
          </div>
        </div>
      )}
      <div className="card">
        {msgs.length > 0 && (
          <div className="row" style={{ gap: 8, alignItems: "center", marginBottom: 8 }}>
            <input
              style={{ width: 220 }}
              placeholder="find in conversation…"
              title="highlight matches in this conversation (F812)"
              value={find}
              onChange={(e) => setFind(e.target.value)}
            />
            {find.trim() !== "" && (
              <span className="muted" style={{ fontSize: 12 }}>
                {(() => {
                  const esc = find.trim().replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
                  const re = new RegExp(esc, "gi");
                  return msgs.reduce((a, m) => a + (m.content.match(re)?.length ?? 0), 0);
                })()}{" "}
                match(es)
              </span>
            )}
            {find !== "" && (
              <button className="btn xs" title="clear the find query" onClick={() => setFind("")}>
                ✕
              </button>
            )}
          </div>
        )}
        <div className="chat">
          {msgs.length === 0 && (
            <div className="empty">select a running model and start chatting</div>
          )}
          {msgs.map((m, i) => (
            <div key={i} className={`msg ${m.role}`}>
              <div className="who">
                {m.role}
                {m.content && (
                  <button
                    className="btn xs copy"
                    title="Copy message"
                    onClick={() => {
                      navigator.clipboard
                        ?.writeText(m.content)
                        .then(() => ok("copied"))
                        .catch(() => err("copy failed"));
                    }}
                  >
                    ⧉
                  </button>
                )}
                {m.content && (
                  <button
                    className="btn xs copy"
                    title="save this message as a prompt template (CLI: llmctl prompts add)"
                    onClick={async () => {
                      const id = window.prompt("Prompt template id:", `chat-${Date.now().toString(36)}`);
                      if (!id || !id.trim()) return;
                      try {
                        await api.createPrompt({ id: id.trim(), name: id.trim(), content: m.content });
                        ok(`prompt ${id.trim()} created`);
                      } catch (e) {
                        err(e instanceof Error ? e.message : String(e));
                      }
                    }}
                  >
                    →P
                  </button>
                )}
              </div>
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
              {m.content ? (
                <div className="md" dangerouslySetInnerHTML={{ __html: highlightFind(renderMd(m.content), find) }} />
              ) : (
                streaming && i === msgs.length - 1 ? "…" : ""
              )}
              {m.tokens && m.tokens.total ? (
                <div className="muted" style={{ fontSize: 11, marginTop: 4 }}>
                  ↑ {m.tokens.prompt} · ↓ {m.tokens.completion} tok
                </div>
              ) : null}
              {!streaming && m.role === "assistant" && i === msgs.length - 1 && (
                <div className="row" style={{ gap: 6, marginTop: 8 }}>
                  <button className="btn sm" onClick={regenerate} title="Re-run the last turn">
                    ↻ Regenerate
                  </button>
                </div>
              )}
              {!streaming && m.role === "user" && i === lastUserIdx && (
                <div className="row" style={{ gap: 6, marginTop: 8 }}>
                  <button className="btn sm" onClick={editLastUser} title="Move this message back into the input">
                    ✎ Edit &amp; resend
                  </button>
                </div>
              )}
            </div>
          ))}
          <div ref={bottomRef} />
        </div>
        {(() => {
          const p = msgs.reduce((a, m) => a + (m.tokens?.prompt || 0), 0);
          const c = msgs.reduce((a, m) => a + (m.tokens?.completion || 0), 0);
          return p + c > 0 ? (
            <div className="muted" style={{ fontSize: 12, marginTop: 6, textAlign: "right" }}>
              conversation total: ↑ {p} · ↓ {c} tok
            </div>
          ) : null;
        })()}
        {pendingApproval && (
          <div
            className="card"
            style={{
              marginTop: 8,
              border: "1px solid var(--warning, #b58900)",
              display: "flex",
              gap: 10,
              alignItems: "center",
              flexWrap: "wrap",
            }}
          >
            <strong>Approve tool call?</strong>
            <span className="mono" style={{ fontSize: 12 }}>
              {pendingApproval.name}({pendingApproval.query})
            </span>
            <div className="row" style={{ gap: 6, marginLeft: "auto" }}>
              <button className="btn sm primary" onClick={() => answerApproval(true)}>
                Approve
              </button>
              <button className="btn sm danger" onClick={() => answerApproval(false)}>
                Deny
              </button>
            </div>
          </div>
        )}
        <div className="row" style={{ marginTop: 12 }}>
          <div style={{ flex: 1, minWidth: 200, display: "flex", flexDirection: "column", gap: 2 }}>
          <textarea
            rows={2}
            style={{ width: "100%" }}
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
          {input.trim() !== "" && (
            <span className="muted" style={{ fontSize: 11 }} title="rough estimate, ~4 chars/token — same rule as llmctl count (F818)">
              ≈{Math.ceil(input.length / 4)} tok
            </span>
          )}
          </div>
          <div style={{ alignSelf: "flex-end" }}>
            {streaming ? (
              <button
                className="btn danger"
                title="stop generating (keeps the partial reply)"
                onClick={() => abortRef.current?.abort()}
              >
                Stop
              </button>
            ) : (
              <button className="btn primary" disabled={!input.trim()} onClick={send}>
                Send
              </button>
            )}
          </div>
        </div>
      </div>
      {forkDialog && (
        <div className="card" style={{ marginTop: 16 }}>
          <h2 style={{ margin: "0 0 12px 0" }}>Fork conversation</h2>
          <div style={{ marginBottom: 10 }}>
            <label style={{ display: "block", fontSize: 12, opacity: 0.7, marginBottom: 4 }}>
              Fork at message
            </label>
            <input
              type="number"
              min={0}
              max={forkDialog.messages}
              value={forkAt}
              onChange={(e) => setForkAt(Math.max(0, Math.min(forkDialog.messages, Number(e.target.value) || 0)))}
              style={{
                width: "100%",
                padding: 6,
                fontSize: 13,
                border: "1px solid var(--border, #333)",
                borderRadius: 4,
                marginBottom: 8,
              }}
            />
            <p style={{ fontSize: 11, opacity: 0.7, margin: 0 }}>
              Keep the first N messages (0 = none, {forkDialog.messages} = all)
            </p>
          </div>
          <div style={{ marginBottom: 10 }}>
            <label style={{ display: "block", fontSize: 12, opacity: 0.7, marginBottom: 4 }}>
              New title
            </label>
            <input
              type="text"
              value={forkTitle}
              onChange={(e) => setForkTitle(e.target.value)}
              placeholder={forkDialog.title}
              style={{
                width: "100%",
                padding: 6,
                fontSize: 13,
                border: "1px solid var(--border, #333)",
                borderRadius: 4,
              }}
            />
          </div>
          <div className="row" style={{ gap: 8 }}>
            <button
              className="btn sm"
              onClick={() => {
                setForkDialog(null);
                setForkAt(0);
                setForkTitle("");
              }}
            >
              Cancel
            </button>
            <button
              className="btn sm primary"
              onClick={() => forkChat(forkDialog.id!, forkAt, forkTitle)}
            >
              Fork
            </button>
          </div>
        </div>
      )}
    </>
  );
}

function isTextFile(contentType: string, filename: string): boolean {
  const ct = contentType.toLowerCase();
  if (ct.startsWith("text/")) return true;
  if (ct === "application/json" || ct === "application/javascript" || ct === "application/xml") return true;
  const ext = filename.toLowerCase().substring(filename.lastIndexOf("."));
  const textExts = [".txt", ".md", ".py", ".js", ".ts", ".json", ".xml", ".html", ".css", ".go", ".rs", ".java", ".c", ".h", ".yml", ".yaml", ".toml", ".ini", ".cfg", ".conf", ".log"];
  return textExts.includes(ext);
}
