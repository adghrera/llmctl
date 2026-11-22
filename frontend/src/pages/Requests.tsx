import { useEffect, useState } from "react";
import { api, type RequestLogEntry } from "../api/client";
import { ok } from "../ui/toast";

function statusTone(s: number): string {
  if (s >= 500) return "err";
  if (s >= 400) return "warn";
  return "run";
}

function csvField(s: string): string {
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

function toCsv(entries: RequestLogEntry[]): string {
  const head = ["id", "time", "model", "key", "status", "stream", "latency_ms", "prompt_tokens", "completion_tokens"];
  const rows = entries.map((e) =>
    [
      e.id || "",
      new Date(e.time).toISOString(),
      e.model || "",
      e.key || "",
      String(e.status),
      e.stream ? "1" : "0",
      String(Math.round(e.latencyMs)),
      String(e.promptTok),
      String(e.compTok),
    ]
      .map(csvField)
      .join(","),
  );
  return [head.join(","), ...rows].join("\n") + "\n";
}

export default function Requests() {
  const [entries, setEntries] = useState<RequestLogEntry[]>([]);
  const [auto, setAuto] = useState(true);
  const [fModel, setFModel] = useState("");
  const [fStatus, setFStatus] = useState("");
  const [fMinMs, setFMinMs] = useState("");
  const [fStream, setFStream] = useState("");

  useEffect(() => {
    let alive = true;
    const load = () =>
      api
        .requests(200, {
          model: fModel.trim() || undefined,
          status: fStatus || undefined,
          minMs: Number(fMinMs) > 0 ? Number(fMinMs) : undefined,
          stream: fStream || undefined,
        })
        .then((d) => alive && setEntries(d))
        .catch(() => {});
    load();
    if (!auto) return () => { alive = false; };
    const t = setInterval(load, 5000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [auto, fModel, fStatus, fMinMs, fStream]);

  return (
    <div className="card">
      <div className="row" style={{ alignItems: "center", marginBottom: 8 }}>
        <h2 style={{ margin: 0 }}>Recent requests</h2>
        <div className="spacer" />
        <button
          className="btn sm"
          disabled={entries.length === 0}
          onClick={() => {
            const blob = new Blob([toCsv(entries)], { type: "text/csv" });
            const a = document.createElement("a");
            a.href = URL.createObjectURL(blob);
            a.download = `llmctl-requests-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
            a.click();
            URL.revokeObjectURL(a.href);
          }}
        >
          Export CSV
        </button>
        <label style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
          <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />
          Auto-refresh
        </label>
      </div>
      <div className="row" style={{ gap: 8, marginBottom: 10, flexWrap: "wrap" }}>
        <input
          style={{ width: 160 }}
          placeholder="model id"
          value={fModel}
          onChange={(e) => setFModel(e.target.value)}
        />
        <select value={fStatus} onChange={(e) => setFStatus(e.target.value)} style={{ width: 110 }}>
          <option value="">any status</option>
          <option value="2">2xx</option>
          <option value="4">4xx</option>
          <option value="5">5xx</option>
        </select>
        <input
          type="number"
          min={0}
          style={{ width: 110 }}
          placeholder="min ms"
          title="minimum latency"
          value={fMinMs}
          onChange={(e) => setFMinMs(e.target.value)}
        />
        <select value={fStream} onChange={(e) => setFStream(e.target.value)} style={{ width: 120 }}>
          <option value="">stream + plain</option>
          <option value="1">streaming</option>
          <option value="0">plain</option>
        </select>
        {(fModel || fStatus || fMinMs || fStream) && (
          <button
            className="btn sm"
            onClick={() => {
              setFModel("");
              setFStatus("");
              setFMinMs("");
              setFStream("");
            }}
          >
            Clear
          </button>
        )}
      </div>
      <p className="muted small" style={{ margin: "0 0 12px" }}>
        Last {entries.length} gateway requests (ring of 256, newest first).
      </p>
      {entries.length === 0 ? (
        <div className="empty">no gateway requests yet</div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Id</th>
              <th>Time</th>
              <th>Model</th>
              <th>Key</th>
              <th>Status</th>
              <th>Latency</th>
              <th>Tokens</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((e, i) => (
              <tr key={i}>
                <td
                  className="mono muted"
                  style={{ fontSize: 11, cursor: e.id ? "pointer" : "default" }}
                  title={e.id ? "click to copy request id" : undefined}
                  onClick={() => {
                    if (e.id && navigator.clipboard) {
                      void navigator.clipboard.writeText(e.id).then(() => ok(`copied ${e.id}`));
                    }
                  }}
                >
                  {e.id || "—"}
                </td>
                <td className="muted" style={{ fontSize: 12 }}>
                  {new Date(e.time).toLocaleTimeString()}
                </td>
                <td className="mono">
                  {e.model || "—"}
                  {e.stream ? <span className="muted"> ⚡</span> : null}
                </td>
                <td className="mono muted">{e.key || "—"}</td>
                <td>
                  <span className={`badge ${statusTone(e.status)}`}>{e.status}</span>
                </td>
                <td>{e.latencyMs < 1000 ? `${Math.round(e.latencyMs)} ms` : `${(e.latencyMs / 1000).toFixed(1)} s`}</td>
                <td className="muted" style={{ fontSize: 12 }}>
                  {e.promptTok} + {e.compTok}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
