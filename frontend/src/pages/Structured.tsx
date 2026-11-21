import { useEffect, useState } from "react";
import { api } from "../api/client";
import { useStore } from "../state/store";
import { err } from "../ui/toast";

const EXAMPLE_SCHEMA = `{
  "type": "object",
  "required": ["title", "year"],
  "properties": {
    "title": { "type": "string" },
    "year": { "type": "integer" },
    "genres": { "type": "array", "items": { "type": "string" } }
  }
}`;

export default function Structured() {
  const { instances } = useStore();
  const running = instances.filter((i) => i.status === "running");
  const [model, setModel] = useState("");
  const [key, setKey] = useState(() => localStorage.getItem("llmctl.chat.key") ?? "");
  const [prompt, setPrompt] = useState("Extract the movie described: 'Dune (2021), a sci-fi epic directed by Denis Villeneuve.'");
  const [schema, setSchema] = useState(EXAMPLE_SCHEMA);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<string>("");
  const [meta, setMeta] = useState<string>("");

  useEffect(() => {
    if (!model && running.length) setModel(running[0].modelId);
  }, [running, model]);

  const run = async () => {
    let parsedSchema: Record<string, unknown>;
    try {
      parsedSchema = JSON.parse(schema);
    } catch (e) {
      err("schema is not valid JSON: " + (e instanceof Error ? e.message : e));
      return;
    }
    if (!prompt.trim()) {
      err("prompt is required");
      return;
    }
    setBusy(true);
    setResult("");
    setMeta("");
    try {
      const r = await api.structured(key, {
        model,
        messages: [{ role: "user", content: prompt }],
        schema: parsedSchema,
      });
      if (r.ok) {
        setResult(JSON.stringify(r.json, null, 2));
        setMeta(`valid ✓ — ${r.attempts} attempt(s)`);
      } else {
        setResult(r.raw || "");
        setMeta(`failed after ${r.attempts} attempts: ${r.error}`);
      }
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="card">
        <h2>Structured output</h2>
        <p className="muted small" style={{ margin: "4px 0 12px" }}>
          Generate JSON that validates against a schema. llmctl retries with
          the validation error appended when the model's reply doesn't match.
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
        <textarea
          rows={3}
          style={{ width: "100%", marginBottom: 8 }}
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="Prompt / text to extract from"
        />
        <textarea
          rows={10}
          style={{ width: "100%", fontFamily: "monospace", fontSize: 12, marginBottom: 8 }}
          value={schema}
          onChange={(e) => setSchema(e.target.value)}
          placeholder="JSON Schema"
        />
        <button className="btn primary" disabled={busy || !model} onClick={run}>
          {busy ? "Running…" : "Generate"}
        </button>
        {!running.length && <span className="muted small" style={{ marginLeft: 10 }}>start an instance first</span>}
      </div>
      {(result || meta) && (
        <div className="card">
          <h2>Result</h2>
          <p className="small" style={{ margin: "4px 0 8px" }}>{meta}</p>
          <pre
            style={{
              fontFamily: "monospace",
              fontSize: 12,
              whiteSpace: "pre-wrap",
              background: "var(--code-background, rgba(0,0,0,0.15))",
              padding: 8,
              borderRadius: 4,
              margin: 0,
            }}
          >
            {result}
          </pre>
        </div>
      )}
    </>
  );
}
