import { useEffect, useRef, useState } from "react";
import { chatStream } from "../api/client";
import { useStore } from "../state/store";
import { err } from "../ui/toast";

interface Msg {
  role: "user" | "assistant";
  content: string;
}

const KEY_STORAGE = "llmctl.chat.key";

export default function Chat() {
  const { instances } = useStore();
  const running = instances.filter((i) => i.status === "running");
  const [model, setModel] = useState("");
  const [key, setKey] = useState(() => localStorage.getItem(KEY_STORAGE) ?? "");
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [showKey, setShowKey] = useState(!key);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    localStorage.setItem(KEY_STORAGE, key);
  }, [key]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [msgs]);

  // Default to the first running instance.
  useEffect(() => {
    if (!model && running.length) setModel(running[0].modelId);
  }, [running, model]);

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
    try {
      // Seed a single assistant message; each delta updates it in place.
      setMsgs((prev) => [...prev, { role: "assistant", content: "" }]);
      for await (const delta of chatStream(key, model, history)) {
        acc += delta;
        setMsgs((prev) => {
          const copy = [...prev];
          copy[copy.length - 1] = { role: "assistant", content: acc };
          return copy;
        });
      }
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setStreaming(false);
    }
  };

  return (
    <>
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
        </div>
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
            placeholder="Type a message (Enter to send, Shift+Enter for newline)"
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
