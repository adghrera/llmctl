import { useEffect, useState } from "react";
import { api, type RequestLogEntry } from "../api/client";

function statusTone(s: number): string {
  if (s >= 500) return "err";
  if (s >= 400) return "warn";
  return "run";
}

function csvField(s: string): string {
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

function toCsv(entries: RequestLogEntry[]): string {
  const head = ["time", "model", "key", "status", "stream", "latency_ms", "prompt_tokens", "completion_tokens"];
  const rows = entries.map((e) =>
    [
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

  useEffect(() => {
    let alive = true;
    const load = () =>
      api
        .requests(200)
        .then((d) => alive && setEntries(d))
        .catch(() => {});
    load();
    if (!auto) return () => { alive = false; };
    const t = setInterval(load, 5000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [auto]);

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
      <p className="muted small" style={{ margin: "0 0 12px" }}>
        Last {entries.length} gateway requests (ring of 256, newest first).
      </p>
      {entries.length === 0 ? (
        <div className="empty">no gateway requests yet</div>
      ) : (
        <table>
          <thead>
            <tr>
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
