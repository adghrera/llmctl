import { useEffect, useState } from "react";
import { api, CostSummary, CostCounter, ApiKey } from "../api/client";
import { err } from "../ui/toast";

export default function Cost() {
  const [summary, setSummary] = useState<CostSummary | null>(null);
  const [keyCosts, setKeyCosts] = useState<Record<string, CostCounter>>({});
  const [modelCosts, setModelCosts] = useState<Record<string, CostCounter>>({});
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [byTag, setByTag] = useState(false); // roll key costs up per tag (CLI `cost -tags` twin, F705)
  const [busy, setBusy] = useState(false);
  const [keyFilter, setKeyFilter] = useState("");
  const [modelFilter, setModelFilter] = useState("");

  const load = async () => {
    setBusy(true);
    try {
      const [s, k, m, kl] = await Promise.all([
        api.costSummary(),
        api.costKeys(),
        api.costModels(),
        api.keys().catch(() => [] as ApiKey[]),
      ]);
      setSummary(s);
      setKeyCosts(k);
      setModelCosts(m);
      setKeys(kl);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    load();
    const t = setInterval(load, 10000);
    return () => clearInterval(t);
  }, []);

  // Cost table text filters: keep rows whose id contains the text
  // (case-insensitive) — UI counterpart of `llmctl cost -model/-key` (F353).
  const match = (id: string, f: string) =>
    !f.trim() || id.toLowerCase().includes(f.trim().toLowerCase());

  // Per-tag roll-up of the key costs (CLI `cost -tags` twin, F705): a key
  // with several tags counts once under each; untagged keys land under
  // "(untagged)".
  const tagRoll = (() => {
    const roll: Record<string, CostCounter> = {};
    const tagsOf: Record<string, string[]> = {};
    for (const k of keys) tagsOf[k.id] = k.tags ?? [];
    for (const [id, c] of Object.entries(keyCosts)) {
      const tags = tagsOf[id]?.length ? tagsOf[id] : ["(untagged)"];
      for (const t of tags) {
        const r = roll[t] ?? { input_tokens: 0, output_tokens: 0, total_tokens: 0, requests: 0, cost_usd: 0, updated_at: "" };
        r.requests += c.requests;
        r.input_tokens += c.input_tokens;
        r.output_tokens += c.output_tokens;
        r.total_tokens += c.total_tokens;
        r.cost_usd += c.cost_usd;
        roll[t] = r;
      }
    }
    return Object.entries(roll).sort((a, b) => b[1].cost_usd - a[1].cost_usd);
  })();

  return (
    <>
      <div className="card">
        <h2>Token Usage & Cost</h2>
        <p style={{ opacity: 0.75, margin: "4px 0 10px" }}>
          Token usage and estimated costs are tracked per API key and per
          model. Costs are estimates based on per-1M-token pricing; actual
          costs may vary. Data refreshes every 10 seconds.
        </p>
        {busy && (
          <p style={{ opacity: 0.7 }}>Loading…</p>
        )}
        {!busy && summary && (
          <div className="row" style={{ gap: 16, marginBottom: 16 }}>
            <div style={{ flex: 1, minWidth: 160 }}>
              <div style={{ fontSize: 12, opacity: 0.7 }}>Total input tokens</div>
              <div style={{ fontSize: 20, fontFamily: "monospace" }}>
                {summary.input_tokens.toLocaleString()}
              </div>
            </div>
            <div style={{ flex: 1, minWidth: 160 }}>
              <div style={{ fontSize: 12, opacity: 0.7 }}>Total output tokens</div>
              <div style={{ fontSize: 20, fontFamily: "monospace" }}>
                {summary.output_tokens.toLocaleString()}
              </div>
            </div>
            <div style={{ flex: 1, minWidth: 160 }}>
              <div style={{ fontSize: 12, opacity: 0.7 }}>Total tokens</div>
              <div style={{ fontSize: 20, fontFamily: "monospace" }}>
                {summary.total_tokens.toLocaleString()}
              </div>
            </div>
            <div style={{ flex: 1, minWidth: 160 }}>
              <div style={{ fontSize: 12, opacity: 0.7 }}>Estimated cost</div>
              <div style={{ fontSize: 20, fontFamily: "monospace" }}>
                {summary.cost_usd.toFixed(4)}
              </div>
            </div>
          </div>
        )}
      </div>

      {Object.keys(keyCosts).length > 0 && (
        <div className="card">
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>{byTag ? "Cost by Key Tag" : "Cost by API Key"}</h2>
            <div className="row" style={{ gap: 6 }}>
            <input
              className="oc-input"
              style={{ width: 160 }}
              placeholder={byTag ? "filter tag…" : "filter key…"}
              value={keyFilter}
              onChange={(e) => setKeyFilter(e.target.value)}
            />
            <button
              className="btn sm"
              title="roll costs up per key tag (CLI: llmctl cost -tags)"
              aria-pressed={byTag}
              onClick={() => setByTag((v) => !v)}
            >
              {byTag ? "By key" : "By tag"}
            </button>
            <button
              className="btn sm"
              onClick={() => {
                // Same columns as `llmctl cost --csv -by key` (F417) and
                // `cost -tags --csv` (F705); filter-aware.
                const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                const rows = byTag
                  ? [
                      "tag,requests,input_tokens,output_tokens,total_tokens,cost_usd",
                      ...tagRoll
                        .filter(([tag]) => match(tag, keyFilter))
                        .map(([tag, c]) =>
                          [tag, String(c.requests), String(c.input_tokens), String(c.output_tokens),
                            String(c.total_tokens), c.cost_usd.toFixed(4)]
                            .map(esc)
                            .join(",")
                        ),
                    ]
                  : [
                      "key,requests,input_tokens,output_tokens,total_tokens,cost_usd",
                      ...Object.entries(keyCosts)
                        .filter(([key]) => match(key, keyFilter))
                        .map(([key, c]) =>
                          [key, String(c.requests), String(c.input_tokens), String(c.output_tokens),
                            String(c.total_tokens), c.cost_usd.toFixed(4)]
                            .map(esc)
                            .join(",")
                        ),
                    ];
                const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-cost-${byTag ? "tags" : "keys"}-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
            <button
              className="btn sm"
              disabled={Object.keys(keyCosts).length === 0}
              title="download the per-key cost counters as JSON (CLI: llmctl cost -json)"
              onClick={() => {
                const blob = new Blob([JSON.stringify(keyCosts, null, 2)], { type: "application/json" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-cost-keys-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export JSON
            </button>
            </div>
          </div>
          {byTag ? (
            <table>
              <thead>
                <tr>
                  <th>Tag</th>
                  <th>Requests</th>
                  <th>Input</th>
                  <th>Output</th>
                  <th>Total</th>
                  <th>Cost (USD)</th>
                </tr>
              </thead>
              <tbody>
                {tagRoll
                  .filter(([tag]) => match(tag, keyFilter))
                  .map(([tag, c]) => (
                  <tr key={tag}>
                    <td>{tag}</td>
                    <td className="oc-num">{c.requests.toLocaleString()}</td>
                    <td className="oc-num">{c.input_tokens.toLocaleString()}</td>
                    <td className="oc-num">{c.output_tokens.toLocaleString()}</td>
                    <td className="oc-num">{c.total_tokens.toLocaleString()}</td>
                    <td className="oc-num">{c.cost_usd.toFixed(4)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
          <table>
            <thead>
              <tr>
                <th>Key (prefix)</th>
                <th>Requests</th>
                <th>Input</th>
                <th>Output</th>
                <th>Total</th>
                <th>Cost (USD)</th>
              </tr>
            </thead>
            <tbody>
              {Object.entries(keyCosts)
                .filter(([key]) => match(key, keyFilter))
                .map(([key, c]) => (
                <tr key={key}>
                  <td style={{ fontFamily: "monospace", fontSize: 12 }}>{key}</td>
                  <td className="oc-num">{c.requests.toLocaleString()}</td>
                  <td className="oc-num">{c.input_tokens.toLocaleString()}</td>
                  <td className="oc-num">{c.output_tokens.toLocaleString()}</td>
                  <td className="oc-num">{c.total_tokens.toLocaleString()}</td>
                  <td className="oc-num">{c.cost_usd.toFixed(4)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          )}
        </div>
      )}

      {Object.keys(modelCosts).length > 0 && (
        <div className="card">
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Cost by Model</h2>
            <div className="row" style={{ gap: 6 }}>
            <input
              className="oc-input"
              style={{ width: 160 }}
              placeholder="filter model…"
              value={modelFilter}
              onChange={(e) => setModelFilter(e.target.value)}
            />
            <button
              className="btn sm"
              onClick={() => {
                // Same columns as `llmctl cost --csv` (F417); filter-aware.
                const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                const rows = [
                  "model,requests,input_tokens,output_tokens,total_tokens,cost_usd",
                  ...Object.entries(modelCosts)
                    .filter(([model]) => match(model, modelFilter))
                    .map(([model, c]) =>
                      [model, String(c.requests), String(c.input_tokens), String(c.output_tokens),
                        String(c.total_tokens), c.cost_usd.toFixed(4)]
                        .map(esc)
                        .join(",")
                    ),
                ];
                const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-cost-models-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
            <button
              className="btn sm"
              disabled={Object.keys(modelCosts).length === 0}
              title="download the per-model cost counters as JSON (CLI: llmctl cost -json)"
              onClick={() => {
                const blob = new Blob([JSON.stringify(modelCosts, null, 2)], { type: "application/json" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-cost-models-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export JSON
            </button>
            </div>
          </div>
          <table>
            <thead>
              <tr>
                <th>Model</th>
                <th>Requests</th>
                <th>Input</th>
                <th>Output</th>
                <th>Total</th>
                <th>Cost (USD)</th>
              </tr>
            </thead>
            <tbody>
              {Object.entries(modelCosts)
                .filter(([model]) => match(model, modelFilter))
                .map(([model, c]) => (
                <tr key={model}>
                  <td style={{ fontFamily: "monospace", fontSize: 12 }}>{model}</td>
                  <td className="oc-num">{c.requests.toLocaleString()}</td>
                  <td className="oc-num">{c.input_tokens.toLocaleString()}</td>
                  <td className="oc-num">{c.output_tokens.toLocaleString()}</td>
                  <td className="oc-num">{c.total_tokens.toLocaleString()}</td>
                  <td className="oc-num">{c.cost_usd.toFixed(4)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {Object.keys(keyCosts).length === 0 && Object.keys(modelCosts).length === 0 && !busy && (
        <div className="card">
          <p style={{ opacity: 0.7 }}>
            No cost data yet. Make some chat requests to generate usage data.
          </p>
        </div>
      )}
    </>
  );
}
