import { Fragment, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, type RequestLogEntry } from "../api/client";
import { ago } from "../lib/time";
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
  const head = ["id", "time", "model", "instance", "key", "status", "stream", "latency_ms", "prompt_tokens", "completion_tokens", "cost_usd", "error"];
  const rows = entries.map((e) =>
    [
      e.id || "",
      new Date(e.time).toISOString(),
      e.model || "",
      e.instance || "",
      e.key || "",
      String(e.status),
      e.stream ? "1" : "0",
      String(Math.round(e.latencyMs)),
      String(e.promptTok),
      String(e.compTok),
      e.cost ? e.cost.toFixed(6) : "",
      e.error || "",
    ]
      .map(csvField)
      .join(","),
  );
  return [head.join(","), ...rows].join("\n") + "\n";
}

export default function Requests() {
  const [params] = useSearchParams();
  const [entries, setEntries] = useState<RequestLogEntry[]>([]);
  const [auto, setAuto] = useState(true);
  const [fModel, setFModel] = useState("");
  const [fStatus, setFStatus] = useState("");
  const [fMinMs, setFMinMs] = useState("");
  const [fStream, setFStream] = useState("");
  const [fKey, setFKey] = useState(params.get("key") || "");
  const [fInstance, setFInstance] = useState(params.get("instance") || "");
  const [fSince, setFSince] = useState("");
  const [fUntil, setFUntil] = useState("");
  const [openIdx, setOpenIdx] = useState<number | null>(null);

  useEffect(() => {
    let alive = true;
    const load = () =>
      api
        .requests(200, {
          model: fModel.trim() || undefined,
          status: fStatus || undefined,
          minMs: Number(fMinMs) > 0 ? Number(fMinMs) : undefined,
          stream: fStream || undefined,
          key: fKey.trim() || undefined,
          instance: fInstance.trim() || undefined,
          since: fSince ? new Date(fSince).toISOString() : undefined,
          until: fUntil ? new Date(fUntil).toISOString() : undefined,
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
  }, [auto, fModel, fStatus, fMinMs, fStream, fKey, fInstance, fSince, fUntil]);

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
          <option value="400">400</option>
          <option value="401">401</option>
          <option value="404">404</option>
          <option value="413">413</option>
          <option value="429">429</option>
          <option value="500">500</option>
          <option value="502">502</option>
          <option value="503">503</option>
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
        <input
          style={{ width: 120 }}
          placeholder="key prefix"
          title="filter by API key prefix"
          value={fKey}
          onChange={(e) => setFKey(e.target.value)}
        />
        <input
          style={{ width: 120 }}
          placeholder="instance id"
          title="filter by serving instance"
          value={fInstance}
          onChange={(e) => setFInstance(e.target.value)}
        />
        <input
          type="datetime-local"
          title="only requests after this time (F189)"
          style={{ width: 180 }}
          value={fSince}
          onChange={(e) => setFSince(e.target.value)}
        />
        <input
          type="datetime-local"
          title="only requests before this time (F189)"
          style={{ width: 180 }}
          value={fUntil}
          onChange={(e) => setFUntil(e.target.value)}
        />
        <select value={fStream} onChange={(e) => setFStream(e.target.value)} style={{ width: 120 }}>
          <option value="">stream + plain</option>
          <option value="1">streaming</option>
          <option value="0">plain</option>
        </select>
        {(fModel || fStatus || fMinMs || fStream || fKey || fInstance || fSince || fUntil) && (
          <button
            className="btn sm"
            onClick={() => {
              setFModel("");
              setFStatus("");
              setFMinMs("");
              setFStream("");
              setFKey("");
              setFInstance("");
              setFSince("");
              setFUntil("");
            }}
          >
            Clear
          </button>
        )}
      </div>
      <p className="muted small" style={{ margin: "0 0 12px" }}>
        Last {entries.length} gateway requests (ring of 256, newest first).
      </p>
      {entries.length > 0 && <StatsBar entries={entries} />}
      {entries.length === 0 ? (
        <div className="empty">no gateway requests yet</div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Id</th>
              <th>Time</th>
              <th>Model</th>
              <th>Instance</th>
              <th>Key</th>
              <th>Status</th>
              <th>Latency</th>
              <th>Tokens</th>
              <th>Cost</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((e, i) => (
              <Fragment key={i}>
                <tr>
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
                    <button
                      className="btn sm"
                      style={{ padding: "0 5px", marginRight: 4 }}
                      title="show full entry"
                      onClick={(ev) => {
                        ev.stopPropagation();
                        setOpenIdx(openIdx === i ? null : i);
                      }}
                    >
                      {openIdx === i ? "▾" : "▸"}
                    </button>
                    {e.id || "—"}
                  </td>
                <td className="muted" style={{ fontSize: 12 }} title={new Date(e.time).toLocaleString()}>
                  {ago(e.time)}
                </td>
                <td className="mono">
                  {e.model || "—"}
                  {e.stream ? <span className="muted"> ⚡</span> : null}
                </td>
                <td className="mono muted">{e.instance || "—"}</td>
                <td className="mono muted">{e.key || "—"}</td>
                <td>
                  <span className={`badge ${statusTone(e.status)}`}>{e.status}</span>
                  {e.error ? (
                    <div className="muted" style={{ fontSize: 11, maxWidth: 260, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={e.error}>
                      {e.error}
                    </div>
                  ) : null}
                </td>
                <td>
                  {e.latencyMs < 1000 ? `${Math.round(e.latencyMs)} ms` : `${(e.latencyMs / 1000).toFixed(1)} s`}
                  {e.ttftMs ? (
                    <span className="muted" title="time to first token">
                      {" "}(ttft {e.ttftMs < 1000 ? `${Math.round(e.ttftMs)}` : `${(e.ttftMs / 1000).toFixed(1)}s`})
                    </span>
                  ) : null}
                </td>
                <td className="muted" style={{ fontSize: 12 }}>
                  {e.promptTok} + {e.compTok}
                </td>
                <td className="mono" style={{ fontSize: 12 }}>
                  {e.cost ? `$${e.cost.toFixed(4)}` : "—"}
                </td>
                </tr>
                {openIdx === i && (
                  <tr>
                    <td colSpan={9} style={{ background: "var(--code-background, rgba(127,127,127,0.08))" }}>
                      <pre style={{ margin: 0, fontSize: 11, overflowX: "auto", whiteSpace: "pre-wrap" }}>
                        {JSON.stringify(e, null, 2)}
                      </pre>
                    </td>
                  </tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

// StatsBar summarizes the visible ring: count, errors, latency percentiles,
// tokens and cost — the UI counterpart of `llmctl requests -stats` (F258).
function StatsBar({ entries }: { entries: RequestLogEntry[] }) {
  const errors = entries.filter((e) => e.status >= 400).length;
  const lat = entries.map((e) => e.latencyMs).sort((a, b) => a - b);
  const pct = (p: number) => (lat.length ? lat[Math.min(lat.length - 1, Math.floor(p * lat.length))] : 0);
  const tokens = entries.reduce((s, e) => s + e.promptTok + e.compTok, 0);
  const cost = entries.reduce((s, e) => s + (e.cost || 0), 0);
  const item = (label: string, value: string) => (
    <span style={{ marginRight: 16 }}>
      <span className="muted">{label}</span> <strong>{value}</strong>
    </span>
  );
  return (
    <p style={{ margin: "0 0 12px", fontSize: 13 }}>
      {item("requests", String(entries.length))}
      {item("errors", String(errors))}
      {item("p50", `${pct(0.5)} ms`)}
      {item("p95", `${pct(0.95)} ms`)}
      {item("tokens", tokens.toLocaleString())}
      {item("cost", cost > 0 ? `$${cost.toFixed(4)}` : "—")}
    </p>
  );
}
