import { useEffect, useState } from "react";
import { api, Instance } from "../api/client";
import { err, ok } from "../ui/toast";

export default function Compare() {
  const [instances, setInstances] = useState<Instance[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [prompt, setPrompt] = useState("");
  const [results, setResults] = useState<Record<string, { content: string; error?: string; done: boolean }>>({});
  const [streaming, setStreaming] = useState(false);
  const [apiKey, setApiKey] = useState(() => localStorage.getItem("llmctl.chat.key") ?? "");

  const load = () =>
    api
      .instances()
      .then((list) => setInstances(list.filter((i) => i.status === "running")))
      .catch((e) => err(e instanceof Error ? e.message : String(e)));

  useEffect(() => {
    load();
  }, []);

  const toggle = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const isSelected = (id: string) => selected.has(id);

  const runComparison = async () => {
    if (!prompt.trim()) {
      err("prompt is required");
      return;
    }
    if (selected.size === 0) {
      err("select at least one instance");
      return;
    }

    setStreaming(true);
    const newResults: Record<string, { content: string; error?: string; done: boolean }> = {};
    for (const id of selected) {
      newResults[id] = { content: "", done: false };
    }
    setResults(newResults);

    const key = apiKey;
    await Promise.all(
      Array.from(selected).map(async (id) => {
        try {
          const inst = instances.find((i) => i.id === id);
          if (!inst) return;
          const body = JSON.stringify({
            model: inst.modelId,
            messages: [{ role: "user", content: prompt }],
            stream: true,
          });
          const resp = await fetch("/v1/chat/completions", {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              Accept: "text/event-stream",
              ...(key ? { Authorization: `Bearer ${key}` } : {}),
            },
            body,
          });
          if (!resp.ok || !resp.body) {
            const t = await resp.text();
            setResults((prev) => ({ ...prev, [id]: { content: "", error: `HTTP ${resp.status}: ${t}`, done: true } }));
            return;
          }
          const reader = resp.body.getReader();
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
              if (data === "[DONE]") continue;
              try {
                const j = JSON.parse(data);
                const delta = j.choices?.[0]?.delta?.content;
                if (delta) {
                  setResults((prev) => ({
                    ...prev,
                    [id]: { ...prev[id], content: (prev[id]?.content || "") + delta, done: false },
                  }));
                }
              } catch { /* partial line */ }
            }
          }
          setResults((prev) => ({ ...prev, [id]: { ...prev[id], done: true } }));
        } catch (e) {
          setResults((prev) => ({ ...prev, [id]: { content: "", error: e instanceof Error ? e.message : String(e), done: true } }));
        }
      })
    );
    setStreaming(false);
    ok("comparison complete");
  };

  const selectedInstances = instances.filter((i) => selected.has(i.id));

  return (
    <>
      <div className="card">
        <h2>Model comparison</h2>
        <p style={{ opacity: 0.75, margin: "4px 0 10px" }}>
          Send the same prompt to multiple running instances and compare
          responses side by side.
        </p>
        <div className="row" style={{ gap: 8, marginBottom: 8 }}>
          <input
            style={{ flex: 1, minWidth: 200 }}
            placeholder="API key (if auth is enabled)"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
          />
          <button className="btn sm" onClick={() => localStorage.setItem("llmctl.chat.key", apiKey)}>
            Save key
          </button>
        </div>
        <div style={{ marginBottom: 10 }}>
          <strong>Select instances:</strong>{" "}
          {selectedInstances.length === 0 && (
            <span style={{ opacity: 0.6, fontSize: 12 }}>(none selected)</span>
          )}
          {selectedInstances.map((inst) => (
            <label
              key={inst.id}
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 6,
                marginRight: 12,
                cursor: "pointer",
              }}
            >
              <input
                type="checkbox"
                checked={isSelected(inst.id)}
                onChange={() => toggle(inst.id)}
              />
              {inst.modelId} ({inst.backendId}){" "}
              {inst.transport === "stdio" ? "stdio" : `port ${inst.port}`}
            </label>
          ))}
        </div>
        <div className="row" style={{ gap: 8, marginBottom: 12 }}>
          <textarea
            style={{ flex: 1, minHeight: 80, fontFamily: "monospace", fontSize: 12 }}
            placeholder="Enter prompt to compare across models…"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
          />
          <button
            className="btn primary"
            disabled={streaming || !prompt.trim() || selected.size === 0}
            onClick={runComparison}
          >
            {streaming ? "Running…" : "Compare"}
          </button>
        </div>
      </div>

      {Object.keys(results).length > 0 && (
        <div className="card">
          <h2>Results</h2>
          <div className="row" style={{ gap: 12 }}>
            {selectedInstances.map((inst) => (
              <div key={inst.id} style={{ flex: 1, minWidth: 200, border: "1px solid var(--border, #333)", borderRadius: 6, overflow: "hidden" }}>
                <div style={{ padding: 8, background: "rgba(127,127,127,0.1)", fontSize: 12, fontWeight: 600, borderBottom: "1px solid var(--border, #333)" }}>
                  {inst.modelId}{" "}
                  <span style={{ opacity: 0.7, fontSize: 10, marginLeft: 6 }}>{inst.backendId}</span>
                </div>
                <div style={{ padding: 10, fontSize: 13, whiteSpace: "pre-wrap", wordBreak: "break-word", minHeight: 200 }}>
                  {results[inst.id]?.error ? (
                    <span style={{ color: "#c44" }}>Error: {results[inst.id].error}</span>
                  ) : results[inst.id]?.done && results[inst.id].content === "" ? (
                    <span style={{ opacity: 0.6 }}>No response</span>
                  ) : (
                    results[inst.id]?.content || (streaming ? "Streaming…" : "")
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </>
  );
}
