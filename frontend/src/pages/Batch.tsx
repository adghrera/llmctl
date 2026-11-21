import { useEffect, useState } from "react";
import { api, type BatchResult } from "../api/client";
import { useStore } from "../state/store";
import { err, ok } from "../ui/toast";

export default function Batch() {
  const { instances } = useStore();
  const running = instances.filter((i) => i.status === "running");
  const [model, setModel] = useState("");
  const [key, setKey] = useState(() => localStorage.getItem("llmctl.chat.key") ?? "");
  const [system, setSystem] = useState("");
  const [prompts, setPrompts] = useState("What is 2+2?\nCapital of France?\nLargest planet?");
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState<BatchResult[] | null>(null);
  const [meta, setMeta] = useState("");

  useEffect(() => {
    if (!model && running.length) setModel(running[0].modelId);
  }, [running, model]);

  const run = async () => {
    const list = prompts
      .split("\n")
      .map((p) => p.trim())
      .filter((p) => p.length > 0);
    if (!list.length) {
      err("enter one prompt per line");
      return;
    }
    setBusy(true);
    setResults(null);
    setMeta("");
    try {
      const r = await api.batch(key, {
        model,
        prompts: list,
        system: system.trim() || undefined,
      });
      setResults(r.results);
      setMeta(`${r.ok} ok, ${r.failed} failed — ${r.concurrency} concurrent`);
      if (r.failed) err(`${r.failed} prompt(s) failed`);
      else ok("batch complete");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="card">
        <h2>Batch inference</h2>
        <p className="muted small" style={{ margin: "4px 0 12px" }}>
          Run many prompts (one per line) through the gateway concurrently.
          Each prompt is an independent single-turn completion.
        </p>
        <div className="row" style={{ gap: 8, marginBottom: 8, flexWrap: "wrap" }}>
          <select value={model} onChange={(e) => setModel(e.target.value)}>
            {running.map((i) => (
              <option key={i.id} value={i.modelId}>
                {i.modelId}
              </option>
            ))}
          </select>
          <input
            style={{ width: 220 }}
            type="password"
            placeholder="API key (if auth enabled)"
            value={key}
            onChange={(e) => setKey(e.target.value)}
          />
        </div>
        <input
          style={{ width: "100%", marginBottom: 8 }}
          placeholder="Optional system prompt"
          value={system}
          onChange={(e) => setSystem(e.target.value)}
        />
        <textarea
          rows={6}
          style={{ width: "100%", marginBottom: 8, fontFamily: "monospace", fontSize: 12 }}
          value={prompts}
          onChange={(e) => setPrompts(e.target.value)}
          placeholder="One prompt per line"
        />
        <button className="btn primary" disabled={busy || !model} onClick={run}>
          {busy ? "Running…" : "Run batch"}
        </button>
        {!running.length && <span className="muted small" style={{ marginLeft: 10 }}>start an instance first</span>}
        {meta && <span className="muted small" style={{ marginLeft: 10 }}>{meta}</span>}
      </div>
      {results && (
        <div className="card">
          <h2>Results</h2>
          <table>
            <thead>
              <tr>
                <th>#</th>
                <th>Prompt</th>
                <th>Output</th>
              </tr>
            </thead>
            <tbody>
              {results.map((r, i) => (
                <tr key={i}>
                  <td>{i + 1}</td>
                  <td style={{ maxWidth: 260 }}>{r.prompt}</td>
                  <td>
                    {r.error ? (
                      <span className="badge err">{r.error}</span>
                    ) : (
                      <span style={{ whiteSpace: "pre-wrap" }}>{r.content}</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
