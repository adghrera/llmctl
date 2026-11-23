import { useEffect, useRef, useState } from "react";
import { api, Instance } from "../api/client";
import { err, ok } from "../ui/toast";

// Client-side twin of `llmctl bench -models A,B` (F671): fire n requests per
// instance with a concurrency pool, measure per-request latency and tokens.

interface BenchRow {
  model: string;
  ok: number;
  fail: number;
  avg: number;
  p50: number;
  p95: number;
  max: number;
  tokps: number;
  reqps: number;
  error?: string;
  done: boolean;
}

const pct = (sorted: number[], p: number) => {
  if (sorted.length === 0) return 0;
  const idx = Math.min(sorted.length - 1, Math.floor((p / 100) * sorted.length));
  return sorted[idx];
};

export default function Bench() {
  const [instances, setInstances] = useState<Instance[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [prompt, setPrompt] = useState(() => localStorage.getItem("llmctl.bench.prompt") ?? "count to five");
  const [n, setN] = useState(() => localStorage.getItem("llmctl.bench.n") ?? "5");
  const [conc, setConc] = useState(() => localStorage.getItem("llmctl.bench.conc") ?? "1");
  const [seed, setSeed] = useState(() => localStorage.getItem("llmctl.bench.seed") ?? "");
  const [topP, setTopP] = useState(() => localStorage.getItem("llmctl.bench.topp") ?? "");
  const [freqPen, setFreqPen] = useState(() => localStorage.getItem("llmctl.bench.freqpen") ?? "");
  const [presPen, setPresPen] = useState(() => localStorage.getItem("llmctl.bench.prespen") ?? "");
  const [rows, setRows] = useState<BenchRow[]>([]);
  const [running, setRunning] = useState(false);
  const abortRef = useRef<AbortController | null>(null); // stop the bench run (F756)
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

  const benchOne = async (inst: Instance, reqs: number, parallel: number): Promise<BenchRow> => {
    const row: BenchRow = { model: inst.modelId, ok: 0, fail: 0, avg: 0, p50: 0, p95: 0, max: 0, tokps: 0, reqps: 0, done: false };
    const times: number[] = [];
    let tokens = 0;
    let next = 0;
    const t0 = performance.now();
    const worker = async () => {
      while (true) {
        if (abortRef.current?.signal.aborted) return; // stop requested (F756)
        const i = next++;
        if (i >= reqs) return;
        const t = performance.now();
        try {
          const resp = await fetch("/v1/chat/completions", {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              ...(apiKey ? { Authorization: `Bearer ${apiKey}` } : {}),
            },
            body: JSON.stringify({
              model: inst.modelId,
              messages: [{ role: "user", content: prompt }],
              ...(seed.trim() !== "" ? { seed: Number(seed) } : {}),
              ...(topP.trim() !== "" ? { top_p: Number(topP) } : {}),
              ...(freqPen.trim() !== "" ? { frequency_penalty: Number(freqPen) } : {}),
              ...(presPen.trim() !== "" ? { presence_penalty: Number(presPen) } : {}),
            }),
            signal: abortRef.current?.signal,
          });
          const ms = performance.now() - t;
          const body = await resp.text();
          if (!resp.ok) {
            row.fail++;
            row.error = row.error || `${resp.status}: ${body.slice(0, 80)}`;
            continue;
          }
          try {
            const j = JSON.parse(body);
            tokens += j?.usage?.total_tokens ?? 0;
          } catch {
            /* SSE or no usage: count the request, no tokens */
          }
          row.ok++;
          times.push(ms);
        } catch (e) {
          if (e instanceof DOMException && e.name === "AbortError") return; // stopped mid-request (F756)
          row.fail++;
          row.error = row.error || String(e);
        }
      }
    };
    await Promise.all(Array.from({ length: Math.max(1, parallel) }, worker));
    const wall = (performance.now() - t0) / 1000;
    times.sort((a, b) => a - b);
    const sum = times.reduce((a, b) => a + b, 0);
    row.avg = times.length ? sum / times.length : 0;
    row.p50 = pct(times, 50);
    row.p95 = pct(times, 95);
    row.max = times.length ? times[times.length - 1] : 0;
    row.tokps = wall > 0 ? tokens / wall : 0;
    row.reqps = wall > 0 ? row.ok / wall : 0;
    row.done = true;
    return row;
  };

  const runBench = async () => {
    if (selected.size === 0) {
      err("select at least one instance");
      return;
    }
    const reqs = Math.max(1, Number(n) || 5);
    const parallel = Math.max(1, Number(conc) || 1);
    setRunning(true);
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    const chosen = instances.filter((i) => selected.has(i.id));
    setRows(chosen.map((i) => ({ model: i.modelId, ok: 0, fail: 0, avg: 0, p50: 0, p95: 0, max: 0, tokps: 0, reqps: 0, done: false })));
    try {
      for (const inst of chosen) {
        const row = await benchOne(inst, reqs, parallel);
        setRows((prev) => prev.map((r) => (r.model === row.model ? row : r)));
        if (ctrl.signal.aborted) {
          ok("bench stopped");
          break;
        }
        if (row.ok === 0) err(`${row.model}: ${row.error || "all requests failed"}`);
        else ok(`${row.model}: ${row.ok}/${reqs} ok, avg ${row.avg.toFixed(0)} ms`);
      }
    } finally {
      abortRef.current = null;
      setRunning(false);
    }
  };

  return (
    <div>
      <h1>Bench</h1>
      <p className="muted" style={{ fontSize: 13 }}>
        Fire repeated requests at running instances and compare latency and throughput —
        the UI twin of <span className="mono">llmctl bench -models A,B</span> (F671).
      </p>
      <div className="card">
        <div className="row" style={{ gap: 8, marginBottom: 8, flexWrap: "wrap" }}>
          <input
            style={{ width: 80 }}
            type="number"
            min={1}
            title="requests per model"
            value={n}
            onChange={(e) => {
              setN(e.target.value);
              localStorage.setItem("llmctl.bench.n", e.target.value);
            }}
          />
          <span className="muted" style={{ alignSelf: "center" }}>requests</span>
          <input
            style={{ width: 80 }}
            type="number"
            min={1}
            title="concurrent requests"
            value={conc}
            onChange={(e) => {
              setConc(e.target.value);
              localStorage.setItem("llmctl.bench.conc", e.target.value);
            }}
          />
          <span className="muted" style={{ alignSelf: "center" }}>concurrent</span>
          <input
            style={{ width: 110 }}
            type="number"
            placeholder="seed"
            title="sampling seed for reproducible bench replies (empty = random) (F796)"
            value={seed}
            onChange={(e) => {
              setSeed(e.target.value);
              localStorage.setItem("llmctl.bench.seed", e.target.value);
            }}
          />
          <input
            style={{ width: 90 }}
            type="number"
            step="0.05"
            min={0}
            max={1}
            placeholder="top_p"
            title="nucleus sampling top_p for bench replies (empty = model default) (F800)"
            value={topP}
            onChange={(e) => {
              setTopP(e.target.value);
              localStorage.setItem("llmctl.bench.topp", e.target.value);
            }}
          />
          <input
            style={{ width: 100 }}
            type="number"
            step="0.1"
            min={-2}
            max={2}
            placeholder="freq penalty"
            title="frequency penalty for bench replies (empty = model default) (F806)"
            value={freqPen}
            onChange={(e) => {
              setFreqPen(e.target.value);
              localStorage.setItem("llmctl.bench.freqpen", e.target.value);
            }}
          />
          <input
            style={{ width: 100 }}
            type="number"
            step="0.1"
            min={-2}
            max={2}
            placeholder="pres penalty"
            title="presence penalty for bench replies (empty = model default) (F806)"
            value={presPen}
            onChange={(e) => {
              setPresPen(e.target.value);
              localStorage.setItem("llmctl.bench.prespen", e.target.value);
            }}
          />
          <input
            style={{ width: 220 }}
            placeholder="API key (if auth on)"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
          />
          <button className="btn sm" onClick={() => localStorage.setItem("llmctl.chat.key", apiKey)}>
            Save key
          </button>
        </div>
        <div style={{ marginBottom: 10 }}>
          <strong>Select instances:</strong>{" "}
          {selected.size === 0 && <span style={{ opacity: 0.6, fontSize: 12 }}>(none selected)</span>}
          {instances.map((inst) => (
            <label key={inst.id} style={{ display: "inline-flex", alignItems: "center", gap: 6, marginRight: 12, cursor: "pointer" }}>
              <input type="checkbox" checked={selected.has(inst.id)} onChange={() => toggle(inst.id)} />
              {inst.modelId} ({inst.backendId})
            </label>
          ))}
        </div>
        <div className="row" style={{ gap: 8, marginBottom: 12 }}>
          <input
            style={{ flex: 1, minWidth: 200 }}
            placeholder="prompt for every request"
            value={prompt}
            onChange={(e) => {
              setPrompt(e.target.value);
              localStorage.setItem("llmctl.bench.prompt", e.target.value);
            }}
          />
          {running ? (
            <button
              className="btn danger"
              title="stop the bench run (finished rows are kept)"
              onClick={() => abortRef.current?.abort()}
            >
              Stop
            </button>
          ) : (
            <button className="btn primary" disabled={selected.size === 0} onClick={runBench}>
              Run bench
            </button>
          )}
          <button
            className="btn sm"
            disabled={selected.size === 0}
            title="copy a curl command for the first selected model (Compare cURL twin, F788)"
            onClick={() => {
              const inst = instances.find((i) => selected.has(i.id));
              if (!inst) return;
              const body = JSON.stringify({ model: inst.modelId, messages: [{ role: "user", content: prompt }], ...(seed.trim() !== "" ? { seed: Number(seed) } : {}), ...(topP.trim() !== "" ? { top_p: Number(topP) } : {}), ...(freqPen.trim() !== "" ? { frequency_penalty: Number(freqPen) } : {}), ...(presPen.trim() !== "" ? { presence_penalty: Number(presPen) } : {}) });
              const q = `'${body.replace(/'/g, `'\\''`)}'`;
              const cmd =
                `curl ${window.location.origin}/v1/chat/completions \\\n` +
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
        </div>
        {rows.length > 0 && (
          <div className="oc-table" style={{ overflowX: "auto" }}>
            <div className="row" style={{ justifyContent: "flex-end", gap: 6, marginBottom: 6 }}>
              <button
                className="btn sm"
                title="download the bench stats as CSV — same columns as llmctl bench --csv (F714)"
                onClick={() => {
                  const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                  const lines = [
                    "model,ok,fails,avg_ms,p50_ms,p95_ms,max_ms,req_per_s,tok_per_s,first_error",
                    ...rows.map((r) =>
                      [
                        r.model,
                        r.done ? String(r.ok) : "",
                        r.done ? String(r.fail) : "",
                        r.done ? r.avg.toFixed(0) : "",
                        r.done ? r.p50.toFixed(0) : "",
                        r.done ? r.p95.toFixed(0) : "",
                        r.done ? r.max.toFixed(0) : "",
                        r.done ? r.reqps.toFixed(2) : "",
                        r.done ? r.tokps.toFixed(0) : "",
                        r.error || "",
                      ]
                        .map(esc)
                        .join(",")
                    ),
                  ];
                  const blob = new Blob([lines.join("\n") + "\n"], { type: "text/csv" });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-bench-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export CSV
              </button>
              <button
                className="btn sm"
                title="download the bench stats as JSON (CLI: llmctl bench -json)"
                onClick={() => {
                  const blob = new Blob([JSON.stringify(rows.map(({ done, ...r }) => r), null, 2)], {
                    type: "application/json",
                  });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-bench-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export JSON
              </button>
            </div>
            <table>
              <thead>
                <tr>
                  <th>MODEL</th>
                  <th>OK</th>
                  <th>FAIL</th>
                  <th>AVG MS</th>
                  <th>P50 MS</th>
                  <th>P95 MS</th>
                  <th>MAX MS</th>
                  <th>TOK/S</th>
                  <th>REQ/S</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.model}>
                    <td className="mono">{r.model}</td>
                    <td>{r.done ? r.ok : "…"}</td>
                    <td>{r.done ? r.fail : ""}</td>
                    <td>{r.done ? r.avg.toFixed(0) : ""}</td>
                    <td>{r.done ? r.p50.toFixed(0) : ""}</td>
                    <td>{r.done ? r.p95.toFixed(0) : ""}</td>
                    <td>{r.done ? r.max.toFixed(0) : ""}</td>
                    <td>{r.done ? r.tokps.toFixed(0) : ""}</td>
                    <td>{r.done ? r.reqps.toFixed(2) : ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {rows.some((r) => r.done && r.error) && (
              <div className="muted" style={{ fontSize: 12, marginTop: 6 }}>
                {rows.filter((r) => r.error).map((r) => (
                  <div key={r.model}>
                    {r.model}: {r.error}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
