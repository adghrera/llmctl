import { useEffect, useState } from "react";
import { api, CostSummary, CostCounter } from "../api/client";
import { err } from "../ui/toast";

export default function Cost() {
  const [summary, setSummary] = useState<CostSummary | null>(null);
  const [keyCosts, setKeyCosts] = useState<Record<string, CostCounter>>({});
  const [modelCosts, setModelCosts] = useState<Record<string, CostCounter>>({});
  const [busy, setBusy] = useState(false);

  const load = async () => {
    setBusy(true);
    try {
      const [s, k, m] = await Promise.all([
        api.costSummary(),
        api.costKeys(),
        api.costModels(),
      ]);
      setSummary(s);
      setKeyCosts(k);
      setModelCosts(m);
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
          <h2>Cost by API Key</h2>
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
              {Object.entries(keyCosts).map(([key, c]) => (
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
        </div>
      )}

      {Object.keys(modelCosts).length > 0 && (
        <div className="card">
          <h2>Cost by Model</h2>
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
              {Object.entries(modelCosts).map(([model, c]) => (
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
