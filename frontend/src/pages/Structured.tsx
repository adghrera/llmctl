import { useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import { useStore } from "../state/store";
import { err, ok } from "../ui/toast";

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
  const [maxTok, setMaxTok] = useState(() => localStorage.getItem("llmctl.structured.maxtok") ?? "");
  const [temp, setTemp] = useState(() => localStorage.getItem("llmctl.structured.temp") ?? "");
  const [seed, setSeed] = useState(() => localStorage.getItem("llmctl.structured.seed") ?? "");
  const [topP, setTopP] = useState(() => localStorage.getItem("llmctl.structured.topp") ?? "");
  const [retries, setRetries] = useState(() => localStorage.getItem("llmctl.structured.retries") ?? ""); // validation retries, CLI -retries twin (F692)
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<string>("");
  const [meta, setMeta] = useState<string>("");
  const schemaFileRef = useRef<HTMLInputElement>(null); // load schema from file (CLI `structured -schema`, F713)

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
        maxTokens: Number(maxTok) > 0 ? Number(maxTok) : undefined,
        temperature: temp.trim() !== "" ? Number(temp) : undefined,
        seed: seed.trim() !== "" ? Number(seed) : undefined,
        topP: topP.trim() !== "" ? Number(topP) : undefined,
        maxRetries: Number(retries) > 0 ? Math.min(Number(retries), 5) : undefined,
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
          <button
            className="btn sm"
            title="remember this key in the browser (shared by Chat/Bench/Batch/Structured/Compare)"
            onClick={() => {
              localStorage.setItem("llmctl.chat.key", key);
              ok("key saved in this browser");
            }}
          >
            Save key
          </button>
        </div>
        <textarea
          rows={3}
          style={{ width: "100%", marginBottom: 8 }}
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="Prompt / text to extract from"
        />
        <div className="row" style={{ gap: 8, marginBottom: 8, flexWrap: "wrap" }}>
          <input
            style={{ width: 130 }}
            type="number"
            min={1}
            placeholder="max tokens"
            title="max tokens per attempt (empty = model default)"
            value={maxTok}
            onChange={(e) => {
              setMaxTok(e.target.value);
              localStorage.setItem("llmctl.structured.maxtok", e.target.value);
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
              localStorage.setItem("llmctl.structured.temp", e.target.value);
            }}
          />
          <input
            style={{ width: 110 }}
            type="number"
            placeholder="seed"
            title="sampling seed for reproducible replies (empty = random) (F795)"
            value={seed}
            onChange={(e) => {
              setSeed(e.target.value);
              localStorage.setItem("llmctl.structured.seed", e.target.value);
            }}
          />
          <input
            style={{ width: 90 }}
            type="number"
            step="0.05"
            min={0}
            max={1}
            placeholder="top_p"
            title="nucleus sampling top_p (empty = model default) (F799)"
            value={topP}
            onChange={(e) => {
              setTopP(e.target.value);
              localStorage.setItem("llmctl.structured.topp", e.target.value);
            }}
          />
          <input
            style={{ width: 130 }}
            type="number"
            min={1}
            max={5}
            placeholder="retries"
            title="validation retries on bad JSON, 1–5 (CLI: llmctl structured -retries, F692)"
            value={retries}
            onChange={(e) => {
              setRetries(e.target.value);
              localStorage.setItem("llmctl.structured.retries", e.target.value);
            }}
          />
        </div>
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
        <button
          className="btn sm"
          style={{ marginLeft: 8 }}
          disabled={!model}
          title="copy a curl command for this structured request (F786)"
          onClick={() => {
            let parsedSchema: unknown = {};
            try {
              parsedSchema = JSON.parse(schema);
            } catch {
              err("schema is not valid JSON");
              return;
            }
            const body: Record<string, unknown> = {
              model,
              messages: [{ role: "user", content: prompt }],
              schema: parsedSchema,
            };
            if (Number(maxTok) > 0) body.maxTokens = Number(maxTok);
            if (temp.trim() !== "") body.temperature = Number(temp);
            if (seed.trim() !== "") body.seed = Number(seed);
            if (topP.trim() !== "") body.topP = Number(topP);
            if (Number(retries) > 0) body.maxRetries = Math.min(Number(retries), 5);
            const json = JSON.stringify(body);
            const q = `'${json.replace(/'/g, `'\\''`)}'`;
            const cmd =
              `curl ${window.location.origin}/api/v1/structured \\\n` +
              `  -H "Authorization: Bearer $LLMCTL_API_KEY" \\\n` +
              `  -H "Content-Type: application/json" \\\n` +
              `  -d ${q}\n`;
            navigator.clipboard
              ?.writeText(cmd)
              .then(() => ok("copied curl command"))
              .catch(() => err("clipboard unavailable"));
          }}
        >
          Copy cURL
        </button>
        <button
          className="btn sm"
          style={{ marginLeft: 8 }}
          title="load the JSON Schema from a .json file (CLI: llmctl structured -schema)"
          onClick={() => schemaFileRef.current?.click()}
        >
          Load schema
        </button>
        <input
          ref={schemaFileRef}
          type="file"
          accept=".json,application/json"
          style={{ display: "none" }}
          onChange={async (e) => {
            const f = e.target.files?.[0];
            e.target.value = "";
            if (!f) return;
            const text = await f.text();
            try {
              setSchema(JSON.stringify(JSON.parse(text), null, 2));
              ok(`loaded schema from ${f.name}`);
            } catch {
              err(`${f.name} is not valid JSON`);
            }
          }}
        />
        {!running.length && <span className="muted small" style={{ marginLeft: 10 }}>start an instance first</span>}
      </div>
      {(result || meta) && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Result</h2>
            {result && (
              <div className="row" style={{ gap: 6 }}>
              <button
                className="btn sm"
                title="copy the validated JSON to the clipboard (F770)"
                onClick={() => {
                  navigator.clipboard
                    ?.writeText(result)
                    .then(() => ok("copied"))
                    .catch(() => {});
                }}
              >
                Copy
              </button>
              <button
                className="btn sm"
                onClick={() => {
                  // Download the validated JSON output (F432).
                  const blob = new Blob([result + "\n"], { type: "application/json" });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-structured-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export JSON
              </button>
              </div>
            )}
          </div>
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
