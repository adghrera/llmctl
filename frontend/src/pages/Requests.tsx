import { Fragment, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, type RequestLogEntry } from "../api/client";
import { ago } from "../lib/time";
import { ok, err } from "../ui/toast";

function statusTone(s: number): string {
  if (s >= 500) return "err";
  if (s >= 400) return "warn";
  return "run";
}

function csvField(s: string): string {
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

// Convert an ISO time URL param to the datetime-local input format (F747).
function fromTimeParam(iso: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
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
  const [params, setParams] = useSearchParams();  const [entries, setEntries] = useState<RequestLogEntry[]>([]);
  const [auto, setAuto] = useState(true);
  const [fModel, setFModel] = useState(params.get("model") || "");
  const [fStatus, setFStatus] = useState(params.get("status") ?? "");
  const [fMinMs, setFMinMs] = useState("");
  const [fStream, setFStream] = useState("");
  const [fKey, setFKey] = useState(params.get("key") || "");
  const [keyNames, setKeyNames] = useState<{ id: string; name: string }[]>([]); // for the key dropdown (F735)
  const [modelIds, setModelIds] = useState<string[]>([]); // for the model dropdown (F736)
  const [instanceIds, setInstanceIds] = useState<string[]>([]); // for the instance dropdown (F736)
  const [fInstance, setFInstance] = useState(params.get("instance") || "");
  const [fSince, setFSince] = useState(fromTimeParam(params.get("since")));
  const [fUntil, setFUntil] = useState(fromTimeParam(params.get("until")));
  const fId = params.get("id") || ""; // single-request deep link (F758)
  const [openIdx, setOpenIdx] = useState<number | null>(null);
  // Clickable column sort over the loaded rows (F774).
  const [sortKey, setSortKey] = useState("");
  const [sortDir, setSortDir] = useState<1 | -1>(-1);
  const [groupBy, setGroupBy] = useState("");

  useEffect(() => {
    api.keys().then(setKeyNames).catch(() => {});
    api.models().then((m) => setModelIds(m.map((x) => x.id).sort())).catch(() => {});
    api.instances().then((i) => setInstanceIds(i.map((x) => x.id).sort())).catch(() => {});
  }, []);

  useEffect(() => {
    let alive = true;
    const load = () => {
      // ?id= shows one logged request by X-Request-Id, twin of `requests -id` (F758).
      if (fId) {
        api
          .requestById(fId)
          .then((e) => {
            if (!alive) return;
            setEntries([e]);
            setOpenIdx(0);
          })
          .catch((e2) => {
            if (!alive) return;
            setEntries([]);
            err(e2 instanceof Error ? e2.message : String(e2));
          });
        return;
      }
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
    };
    load();
    if (!auto) return () => { alive = false; };
    const t = setInterval(load, 5000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [auto, fModel, fStatus, fMinMs, fStream, fKey, fInstance, fSince, fUntil, fId]);

  const toggleSort = (k: string) => {
    if (sortKey === k) setSortDir((d) => (d === 1 ? -1 : 1));
    else {
      setSortKey(k);
      setSortDir(k === "model" || k === "instance" || k === "key" ? 1 : -1);
    }
  };
  const sortedEntries = (() => {
    if (!sortKey) return entries;
    const val = (e: RequestLogEntry): string | number => {
      switch (sortKey) {
        case "time": return e.time;
        case "model": return e.model || "";
        case "instance": return e.instance || "";
        case "key": return e.key || "";
        case "status": return e.status;
        case "latency": return e.latencyMs;
        case "tokens": return (e.promptTok || 0) + (e.compTok || 0);
        case "cost": return e.cost || 0;
        default: return 0;
      }
    };
    return [...entries].sort((a, b) => {
      const va = val(a);
      const vb = val(b);
      const c = typeof va === "number" && typeof vb === "number" ? va - vb : String(va).localeCompare(String(vb));
      return c * sortDir;
    });
  })();
  const arrow = (k: string) => (sortKey === k ? (sortDir === 1 ? " ▲" : " ▼") : "");

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
        <button
          className="btn sm"
          disabled={entries.length === 0}
          title="download the request log as JSON (CLI: llmctl requests -json)"
          onClick={() => {
            const blob = new Blob([JSON.stringify(entries, null, 2)], { type: "application/json" });
            const a = document.createElement("a");
            a.href = URL.createObjectURL(blob);
            a.download = `llmctl-requests-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
            a.click();
            URL.revokeObjectURL(a.href);
          }}
        >
          Export JSON
        </button>
        <label style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
          <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />
          Auto-refresh
        </label>
      </div>
      <div className="row" style={{ gap: 8, marginBottom: 10, flexWrap: "wrap" }}>
        {modelIds.length > 0 && (
          <select
            style={{ width: 150 }}
            title="filter by model id (CLI: llmctl requests -model, F736)"
            value={modelIds.includes(fModel) ? fModel : ""}
            onChange={(e) => setFModel(e.target.value)}
          >
            <option value="">all models</option>
            {modelIds.map((id) => (
              <option key={id} value={id}>
                {id}
              </option>
            ))}
          </select>
        )}
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
        {keyNames.length > 0 && (
          <select
            style={{ width: 150 }}
            title="filter by API key name — sets the key-id filter (CLI: llmctl requests -key, F735)"
            value={keyNames.some((k) => k.id === fKey) ? fKey : ""}
            onChange={(e) => setFKey(e.target.value)}
          >
            <option value="">all keys</option>
            {keyNames.map((k) => (
              <option key={k.id} value={k.id}>
                {k.name || k.id}
              </option>
            ))}
          </select>
        )}
        <input
          style={{ width: 120 }}
          placeholder="key prefix"
          title="filter by API key prefix"
          value={fKey}
          onChange={(e) => setFKey(e.target.value)}
        />
        {instanceIds.length > 0 && (
          <select
            style={{ width: 150 }}
            title="filter by serving instance id (CLI: llmctl requests -instance, F736)"
            value={instanceIds.includes(fInstance) ? fInstance : ""}
            onChange={(e) => setFInstance(e.target.value)}
          >
            <option value="">all instances</option>
            {instanceIds.map((id) => (
              <option key={id} value={id}>
                {id}
              </option>
            ))}
          </select>
        )}
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
        {(fModel || fStatus || fMinMs || fStream || fKey || fInstance || fSince || fUntil || fId) && (
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
              if (fId) {
                const next = new URLSearchParams(params);
                next.delete("id");
                setParams(next, { replace: true });
              }
            }}
          >
            Clear
          </button>
        )}
      </div>
      <p className="muted small" style={{ margin: "0 0 12px" }}>
        Last {entries.length} gateway requests (ring of 256, newest first).{" "}
        {entries.length > 0 && (
          <label style={{ marginLeft: 4 }}>
            group stats{" "}
            <select value={groupBy} onChange={(ev) => setGroupBy(ev.target.value)}>
              <option value="">off</option>
              <option value="day">by day</option>
              <option value="hour">by hour</option>
              <option value="status">by status</option>
              <option value="model">by model</option>
              <option value="key">by key</option>
              <option value="instance">by instance</option>
            </select>
          </label>
        )}
      </p>
      {entries.length > 0 && <StatsBar entries={entries} />}
      {entries.length > 0 && groupBy && <GroupStats entries={entries} by={groupBy} />}
      {entries.length === 0 ? (
        <div className="empty">no gateway requests yet</div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Id</th>
              <th style={{ cursor: "pointer" }} title="sort by time (F774)" onClick={() => toggleSort("time")}>Time{arrow("time")}</th>
              <th style={{ cursor: "pointer" }} title="sort by model (F774)" onClick={() => toggleSort("model")}>Model{arrow("model")}</th>
              <th style={{ cursor: "pointer" }} title="sort by instance (F774)" onClick={() => toggleSort("instance")}>Instance{arrow("instance")}</th>
              <th style={{ cursor: "pointer" }} title="sort by key (F774)" onClick={() => toggleSort("key")}>Key{arrow("key")}</th>
              <th style={{ cursor: "pointer" }} title="sort by status (F774)" onClick={() => toggleSort("status")}>Status{arrow("status")}</th>
              <th style={{ cursor: "pointer" }} title="sort by latency (F774)" onClick={() => toggleSort("latency")}>Latency{arrow("latency")}</th>
              <th style={{ cursor: "pointer" }} title="sort by tokens (F774)" onClick={() => toggleSort("tokens")}>Tokens{arrow("tokens")}</th>
              <th style={{ cursor: "pointer" }} title="sort by cost (F774)" onClick={() => toggleSort("cost")}>Cost{arrow("cost")}</th>
            </tr>
          </thead>
          <tbody>
            {sortedEntries.map((e, i) => (
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
                      <button
                        className="btn sm"
                        style={{ margin: "4px 0" }}
                        title="copy this request's JSON detail to the clipboard (F757)"
                        onClick={() => {
                          navigator.clipboard
                            ?.writeText(JSON.stringify(e, null, 2))
                            .then(() => ok("copied"))
                            .catch(() => {});
                        }}
                      >
                        Copy JSON
                      </button>
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
  const exportCsv = () => {
    // Same columns as `llmctl requests --stats --csv` (F421).
    const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
    const avg = lat.length ? lat.reduce((s, l) => s + l, 0) / lat.length : 0;
    const max = lat.length ? lat[lat.length - 1] : 0;
    // CLI percentile index: lats[floor(p*(n-1))] — match `requests --stats`.
    const cpct = (p: number) => (lat.length ? lat[Math.floor(p * (lat.length - 1))] : 0);
    const inTok = entries.reduce((s, e) => s + e.promptTok, 0);
    const outTok = entries.reduce((s, e) => s + e.compTok, 0);
    const errPct = entries.length ? (100 * errors) / entries.length : 0;
    const rows = [
      "group,requests,errors,error_pct,avg_ms,p50_ms,p95_ms,max_ms,in_tokens,out_tokens,cost_usd",
      ["(all)", String(entries.length), String(errors), errPct.toFixed(1), avg.toFixed(0),
        cpct(0.5).toFixed(0), cpct(0.95).toFixed(0), max.toFixed(0), String(inTok), String(outTok), cost.toFixed(4)]
        .map(esc).join(","),
    ];
    const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `llmctl-requests-stats-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
  };
  const exportJson = () => {
    // Same fields as `llmctl requests --stats --csv`, as one JSON object (F729).
    const avg = lat.length ? lat.reduce((s, l) => s + l, 0) / lat.length : 0;
    const max = lat.length ? lat[lat.length - 1] : 0;
    const cpct = (p: number) => (lat.length ? lat[Math.floor(p * (lat.length - 1))] : 0);
    const obj = {
      group: "(all)",
      requests: entries.length,
      errors,
      error_pct: entries.length ? (100 * errors) / entries.length : 0,
      avg_ms: Math.round(avg),
      p50_ms: cpct(0.5),
      p95_ms: cpct(0.95),
      max_ms: max,
      in_tokens: entries.reduce((s, e) => s + e.promptTok, 0),
      out_tokens: entries.reduce((s, e) => s + e.compTok, 0),
      cost_usd: Number(cost.toFixed(6)),
    };
    const blob = new Blob([JSON.stringify(obj, null, 2)], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `llmctl-requests-stats-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  };
  return (
    <p style={{ margin: "0 0 12px", fontSize: 13 }}>
      {item("requests", String(entries.length))}
      {item("errors", String(errors))}
      {item("p50", `${pct(0.5)} ms`)}
      {item("p95", `${pct(0.95)} ms`)}
      {item("tokens", tokens.toLocaleString())}
      {item("cost", cost > 0 ? `$${cost.toFixed(4)}` : "—")}
      <button className="btn sm" style={{ marginLeft: 8 }} onClick={exportCsv}>
        Export CSV
      </button>
      <button className="btn sm" style={{ marginLeft: 6 }} title="download the stats summary as JSON (CLI: llmctl requests --stats -json)" onClick={exportJson}>
        Export JSON
      </button>
    </p>
  );
}

// GroupStats groups the visible ring by day/status/model/key/instance — the
// UI counterpart of `llmctl requests -stats -by …` (F549, F554). Same
// percentile index as the CLI.
function GroupStats({ entries, by }: { entries: RequestLogEntry[]; by: string }) {
  const keyOf = (e: RequestLogEntry): string => {
    switch (by) {
      case "day":
        return e.time.slice(0, 10); // RFC3339 UTC → calendar day
      case "hour": // UTC hour bucket, matching `requests -stats -by hour` (F582, F594)
        return new Date(e.time).toISOString().slice(0, 13).replace("T", " ") + ":00";
      case "status":
        return String(e.status);
      case "model":
        return e.model || "—";
      case "key":
        return e.key || "—";
      case "instance":
        return e.instance || "—";
      default:
        return "—";
    }
  };
  const groups = new Map<string, RequestLogEntry[]>();
  for (const e of entries) {
    const d = keyOf(e);
    const g = groups.get(d);
    if (g) g.push(e);
    else groups.set(d, [e]);
  }
  const keys = [...groups.keys()].sort();
  if (by === "day" || by === "hour") keys.reverse();
  const label = { day: "Day", hour: "Hour", status: "Status", model: "Model", key: "Key", instance: "Instance" }[by] || "Group";
  return (
    <table style={{ marginBottom: 12 }}>
      <thead>
        <tr>
          <th>{label}</th>
          <th>Reqs</th>
          <th>Errors</th>
          <th>P50</th>
          <th>P95</th>
          <th>In</th>
          <th>Out</th>
          <th>Cost</th>
        </tr>
      </thead>
      <tbody>
        {keys.map((d) => {
          const es = groups.get(d)!;
          const errors = es.filter((e) => e.status >= 400).length;
          const lat = es.map((e) => e.latencyMs).sort((a, b) => a - b);
          const pct = (p: number) => (lat.length ? lat[Math.floor(p * (lat.length - 1))] : 0);
          const inTok = es.reduce((s, e) => s + e.promptTok, 0);
          const outTok = es.reduce((s, e) => s + e.compTok, 0);
          const cost = es.reduce((s, e) => s + (e.cost || 0), 0);
          return (
            <tr key={d}>
              <td className="mono">{d}</td>
              <td>{es.length}</td>
              <td>
                {errors > 0 ? <span className={`badge ${errors === es.length ? "err" : "warn"}`}>{errors}</span> : <span className="muted">0</span>}
              </td>
              <td>{pct(0.5).toFixed(0)} ms</td>
              <td>{pct(0.95).toFixed(0)} ms</td>
              <td className="muted">{inTok.toLocaleString()}</td>
              <td className="muted">{outTok.toLocaleString()}</td>
              <td className="mono">{cost ? `$${cost.toFixed(4)}` : "—"}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
