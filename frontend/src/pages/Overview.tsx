import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { api, type GatewayMetrics, type GPUInfo } from "../api/client";
import { ago } from "../lib/time";
import { useStore } from "../state/store";

export default function Overview() {
  const { status, instances, jobs, models, refresh } = useStore();
  const [metrics, setMetrics] = useState<GatewayMetrics | null>(null);
  const [gpus, setGpus] = useState<GPUInfo[] | null>(null);
  const [disk, setDisk] = useState<Awaited<ReturnType<typeof api.disk>> | null>(null);
  const [events, setEvents] = useState<Awaited<ReturnType<typeof api.events>>>([]);
  const [eventKind, setEventKind] = useState("");
  const [eventStats, setEventStats] = useState(false); // per-kind rollup (CLI `events -stats` parity, F600)
  const [eventKinds, setEventKinds] = useState<string[]>([]);
  const [eventModel, setEventModel] = useState("");
  const [eventKey, setEventKey] = useState(""); // key.* filter by key name (F696)
  const [today, setToday] = useState<{ requests: number; errors: number; tokens: number; cost: number } | null>(null);
  const [daemonLog, setDaemonLog] = useState<string | null>(null);
  const [daemonFollow, setDaemonFollow] = useState(false);
  const [daemonLogFilter, setDaemonLogFilter] = useState("");
  const daemonPreRef = useRef<HTMLPreElement>(null);
  const [health, setHealth] = useState<Awaited<ReturnType<typeof api.doctor>> | null>(null);

  // Follow mode for the daemon log card: re-tail every 2s while on,
  // the UI counterpart of `llmctl logs -daemon -f` (F423).
  useEffect(() => {
    if (!daemonFollow || daemonLog === null) return;
    const t = setInterval(() => {
      api.daemonLog().then((r) => setDaemonLog(r.logs)).catch(() => {});
    }, 2000);
    return () => clearInterval(t);
  }, [daemonFollow, daemonLog === null]);

  useEffect(() => {
    if (daemonFollow && daemonPreRef.current) {
      daemonPreRef.current.scrollTop = daemonPreRef.current.scrollHeight;
    }
  }, [daemonLog, daemonFollow]);

  useEffect(() => {
    let alive = true;
    const tick = () =>
      api
        .doctor()
        .then((d) => alive && setHealth(d))
        .catch(() => {});
    tick();
    const t = setInterval(tick, 60000); // health changes slowly
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);

  useEffect(() => {
    let alive = true;
    const tick = () => {
      const day = new Date().toISOString().slice(0, 10);
      api
        .dailyUsage(2)
        .then((d) => {
          if (!alive) return;
          const models = d[day];
          if (!models) return setToday(null);
          let requests = 0, errors = 0, tokens = 0, cost = 0;
          for (const u of Object.values(models)) {
            requests += u.requests;
            errors += u.errors;
            tokens += u.prompt + u.comp;
            cost += u.cost ?? 0;
          }
          setToday({ requests, errors, tokens, cost });
        })
        .catch(() => {});
    };
    tick();
    const t = setInterval(tick, 30000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);

  useEffect(() => {
    let alive = true;
    const tick = () =>
      api
        .events(50, eventKind, eventModel, eventKey)
        .then((e) => {
          if (!alive) return;
          setEvents(e);
          setEventKinds((prev) => [...new Set([...prev, ...e.map((x) => x.kind)])]);
        })
        .catch(() => {});
    tick();
    const t = setInterval(tick, 10000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [eventKind, eventModel, eventKey]);

  useEffect(() => {
    let alive = true;
    const tick = () =>
      api
        .metrics()
        .then((m) => alive && setMetrics(m))
        .catch(() => alive && setMetrics(null));
    tick();
    const t = setInterval(tick, 5000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);

  useEffect(() => {
    api.gpu().then((r) => setGpus(r.gpus)).catch(() => setGpus([]));
    api.disk().then(setDisk).catch(() => setDisk(null));
  }, []);

  const running = instances.filter((i) => i.status === "running");
  const activeJobs = jobs.filter((j) => j.status === "running");
  const [jobStats, setJobStats] = useState(false); // per-kind roll-up, CLI `jobs -stats` twin (F752)
  const completedJobs = jobs
    .filter(j => j.status === "done" || j.status === "error")
    .sort((a, b) => new Date(b.created).getTime() - new Date(a.created).getTime())
    .slice(0, 5);

  return (
    <>
      <div className="card">
        <h2>Status</h2>
        {status ? (
          <p>
            llmctl <span className="mono">{status.version}</span> —{" "}
            {status.backends} backends, {status.models} models,{" "}
            {status.instances} instances.
          </p>
        ) : (
          <div className="empty">connecting to daemon…</div>
        )}
      </div>
      {health && (
        <div className="card">
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Health</h2>
            <div className="row" style={{ gap: 6 }}>
              <button
                className="btn sm"
                title="download the doctor checks as CSV — same columns as llmctl doctor --csv (F723)"
                onClick={() => {
                  const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                  const rows = [
                    "name,status,detail",
                    ...health.checks.map((c) => [c.name, c.status, c.detail || ""].map(esc).join(",")),
                  ];
                  const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-doctor-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export CSV
              </button>
              <button
                className="btn sm"
                title="download the doctor checks as JSON (CLI: llmctl doctor -json)"
                onClick={() => {
                  const blob = new Blob([JSON.stringify(health, null, 2)], { type: "application/json" });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-doctor-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export JSON
              </button>
            </div>
          </div>
          {health.checks.filter((c) => c.status !== "ok").length === 0 ? (
            <p>
              <span className="badge ok">ok</span> all {health.checks.length} checks passed
            </p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Check</th>
                  <th>Detail</th>
                </tr>
              </thead>
              <tbody>
                {health.checks
                  .filter((c) => c.status !== "ok")
                  .map((c) => (
                    <tr key={c.name}>
                      <td>
                        <span className={`badge ${c.status === "error" ? "err" : "warn"}`}>{c.status}</span>{" "}
                        <span className="mono" style={{ fontSize: 12 }}>{c.name}</span>
                      </td>
                      <td className="muted" style={{ fontSize: 12 }}>{c.detail}</td>
                    </tr>
                  ))}
              </tbody>
            </table>
          )}
        </div>
      )}
      {today && (
        <div className="card">
          <h2>Today (UTC)</h2>
          <div className="row" style={{ gap: 24, flexWrap: "wrap" }}>
            <Stat label="requests" value={today.requests} to={`/requests?since=${encodeURIComponent(`${new Date().toISOString().slice(0, 10)}T00:00:00Z`)}`} />
            <Stat label="errors" value={today.errors} />
            <Stat label="tokens" value={today.tokens.toLocaleString()} />
            <Stat label="est. cost" value={today.cost > 0 ? `$${today.cost.toFixed(4)}` : "—"} />
          </div>
        </div>
      )}
      <div className="card">
        <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>GPU</h2>
          {gpus && gpus.length > 0 && (
            <div className="row" style={{ gap: 6 }}>
              <button
                className="btn sm"
                title="download the GPU list as CSV — same columns as llmctl gpu --csv (F721)"
                onClick={() => {
                  const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                  const rows = [
                    "vendor,name,total_bytes,used_bytes,used_pct",
                    ...gpus.map((g) =>
                      [
                        g.vendor, g.name, String(g.total_bytes), String(g.used_bytes),
                        g.total_bytes > 0 ? ((g.used_bytes / g.total_bytes) * 100).toFixed(1) : "0",
                      ]
                        .map(esc)
                        .join(",")
                    ),
                  ];
                  const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-gpu-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export CSV
              </button>
              <button
                className="btn sm"
                title="download the GPU list as JSON (CLI: llmctl gpu -json)"
                onClick={() => {
                  const blob = new Blob([JSON.stringify(gpus, null, 2)], { type: "application/json" });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-gpu-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export JSON
              </button>
            </div>
          )}
        </div>
        {gpus === null ? (
          <div className="empty">detecting…</div>
        ) : gpus.length === 0 ? (
          <div className="empty">no GPUs detected (nvidia-smi / rocm-smi not found or reported none)</div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>name</th>
                <th>vendor</th>
                <th>VRAM used</th>
                <th>VRAM total</th>
              </tr>
            </thead>
            <tbody>
              {gpus.map((g) => (
                <tr key={g.vendor + g.name}>
                  <td className="mono">{g.name}</td>
                  <td>{g.vendor}</td>
                  <td>{fmtBytes(g.used_bytes)}</td>
                  <td>{fmtBytes(g.total_bytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <div className="card">
        <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Disk</h2>
          {disk && (
            <div className="row" style={{ gap: 6 }}>
              <button
                className="btn sm"
                title="download the disk report as CSV — same columns as llmctl disk --csv (F722)"
                onClick={() => {
                  const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                  const rows = [
                    "dir,models_count,models_bytes,free_bytes,total_bytes",
                    [
                      disk.dir, String(disk.modelsCount), String(disk.modelsBytes),
                      String(disk.freeBytes ?? 0), String(disk.totalBytes ?? 0),
                    ]
                      .map(esc)
                      .join(","),
                  ];
                  const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-disk-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export CSV
              </button>
              <button
                className="btn sm"
                title="download the disk report as JSON (CLI: llmctl disk -json)"
                onClick={() => {
                  const blob = new Blob([JSON.stringify(disk, null, 2)], { type: "application/json" });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-disk-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export JSON
              </button>
            </div>
          )}
        </div>
        {disk ? (
          <p>
            {disk.modelsCount} model{disk.modelsCount === 1 ? "" : "s"} using{" "}
            <span className="mono">{fmtBytes(disk.modelsBytes)}</span>
            {disk.freeBytes !== undefined && (
              <> — {fmtBytes(disk.freeBytes)} free of {fmtBytes(disk.totalBytes ?? 0)}</>
            )}
            <span className="muted"> ({disk.dir})</span>
          </p>
        ) : (
          <div className="empty">disk info unavailable</div>
        )}
      </div>
      <div className="card">
        <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Gateway metrics</h2>
          {metrics && Object.keys(metrics.models).length > 0 && (
            <div className="row" style={{ gap: 6 }}>
              <button
                className="btn sm"
                title="download the per-model metrics table as CSV — same columns as llmctl metrics --csv (F722)"
                onClick={() => {
                  const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                  const rows = [
                    "model,requests,errors,prompt_tokens,completion_tokens",
                    ...Object.entries(metrics.models).map(([id, m]) =>
                      [
                        id, String(m.requests), String(m.errors),
                        String(m.prompt_tokens), String(m.completion_tokens),
                      ]
                        .map(esc)
                        .join(",")
                    ),
                  ];
                  const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-metrics-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export CSV
              </button>
              <button
                className="btn sm"
                title="download the metrics JSON (CLI: llmctl metrics -json)"
                onClick={() => {
                  const blob = new Blob([JSON.stringify(metrics, null, 2)], { type: "application/json" });
                  const a = document.createElement("a");
                  a.href = URL.createObjectURL(blob);
                  a.download = `llmctl-metrics-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                  a.click();
                  URL.revokeObjectURL(a.href);
                }}
              >
                Export JSON
              </button>
            </div>
          )}
        </div>
        {metrics ? (
          <>
            <div className="row" style={{ gap: 24, flexWrap: "wrap" }}>
              <Stat label="requests" value={metrics.requests_total} />
              <Stat label="in-flight" value={metrics.inflight} />
              <Stat label="prompt tokens" value={metrics.tokens.prompt} />
              <Stat label="completion tokens" value={metrics.tokens.completion} />
              <Stat label="p50" value={fmtMs(metrics.latency.p50_ms)} />
              <Stat label="p90" value={fmtMs(metrics.latency.p90_ms)} />
              <Stat label="p99" value={fmtMs(metrics.latency.p99_ms)} />
            </div>
            {Object.keys(metrics.models).length > 0 && (
              <table className="table" style={{ marginTop: 12 }}>
                <thead>
                  <tr>
                    <th>model</th>
                    <th>requests</th>
                    <th>errors</th>
                    <th>prompt tok</th>
                    <th>compl tok</th>
                  </tr>
                </thead>
                <tbody>
                  {Object.entries(metrics.models).map(([name, m]) => (
                    <tr key={name}>
                      <td className="mono">{name}</td>
                      <td>{m.requests}</td>
                      <td>{m.errors > 0 ? <span className="danger">{m.errors}</span> : 0}</td>
                      <td>{m.prompt_tokens}</td>
                      <td>{m.completion_tokens}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        ) : (
          <div className="empty">no metrics yet</div>
        )}
      </div>
      <div className="card">
        <h2>Running instances</h2>
        {running.length ? (
          <ul>
            {running.map((i) => (
              <li key={i.id} className="mono">
                {i.id} <span className="muted">({i.backendId}/{i.transport})</span>
              </li>
            ))}
          </ul>
        ) : (
          <div className="empty">no instances running</div>
        )}
      </div>
      {(events.length > 0 || eventKind || eventModel || eventKey) && (
        <div className="card">
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Events</h2>
            <button
              className="btn sm"
              aria-pressed={eventStats}
              title="group the feed per kind — CLI: llmctl events -stats (F600)"
              onClick={() => setEventStats(!eventStats)}
            >
              {eventStats ? "Show list" : "Stats"}
            </button>
            <button
              className="btn sm"
              disabled={events.length === 0}
              onClick={() => {
                // Same columns as `llmctl events --csv` (F371).
                const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                const rows = [
                  ["time", "kind", "instance", "model", "key"].join(","),
                  ...events.map((e) =>
                    [new Date(e.time).toISOString(), e.kind || "", e.instance || "", e.model || "", e.key || ""]
                      .map(esc)
                      .join(",")
                  ),
                ];
                const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-events-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
            <button
              className="btn sm"
              disabled={events.length === 0}
              title="download the event feed as JSON (CLI: llmctl events -json)"
              onClick={() => {
                const blob = new Blob([JSON.stringify(events, null, 2)], { type: "application/json" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-events-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export JSON
            </button>
            <label className="muted small" style={{ display: "flex", alignItems: "center", gap: 6 }}>
              model
              <select
                style={{ width: 140 }}
                value={eventModel}
                onChange={(e) => setEventModel(e.target.value)}
              >
                <option value="">all</option>
                {models.map((m) => (
                  <option key={m.id} value={m.id}>{m.id}</option>
                ))}
              </select>
            </label>
            <label className="muted small" style={{ display: "flex", alignItems: "center", gap: 6 }}>
              kind
              <select
                style={{ width: 180 }}
                value={eventKind}
                onChange={(e) => setEventKind(e.target.value)}
              >
                <option value="">all</option>
                {eventKinds.map((k) => (
                  <option key={k} value={k}>{k}</option>
                ))}
              </select>
            </label>
            <label className="muted small" style={{ display: "flex", alignItems: "center", gap: 6 }}>
              key
              <input
                style={{ width: 120 }}
                placeholder="key name"
                title="only key.* alerts for this key name — CLI: llmctl events -key (F696)"
                value={eventKey}
                onChange={(e) => setEventKey(e.target.value)}
              />
            </label>
          </div>
          {events.length === 0 && <p className="muted small">No events of this kind.</p>}
          {eventStats ? (
            <table>
              <thead>
                <tr>
                  <th>Event</th>
                  <th>Count</th>
                  <th>Last</th>
                  <th>Instances / Models</th>
                </tr>
              </thead>
              <tbody>
                {(() => {
                  const by = new Map<string, { count: number; last: string; who: Set<string> }>();
                  for (const e of events) {
                    const g = by.get(e.kind) ?? { count: 0, last: "", who: new Set<string>() };
                    g.count++;
                    if (e.time > g.last) g.last = e.time;
                    if (e.instance) g.who.add(e.instance);
                    if (e.key) g.who.add(e.key);
                    if (e.model) g.who.add(e.model);
                    by.set(e.kind, g);
                  }
                  return [...by.entries()]
                    .sort((a, b) => b[1].count - a[1].count)
                    .map(([kind, g]) => (
                      <tr key={kind}>
                        <td>
                          <span className={`badge ${kind.includes("crashed") ? "err" : "warn"}`}>
                            {kind.replace("instance.", "")}
                          </span>
                        </td>
                        <td>{g.count}</td>
                        <td className="muted" style={{ fontSize: 12 }} title={new Date(g.last).toLocaleString()}>
                          {ago(g.last)}
                        </td>
                        <td className="mono" style={{ fontSize: 12 }}>{[...g.who].join(" ") || "—"}</td>
                      </tr>
                    ));
                })()}
              </tbody>
            </table>
          ) : (
          <table>
            <thead>
              <tr>
                <th>Time</th>
                <th>Event</th>
                <th>Instance</th>
                <th>Model</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e, i) => (
                <tr key={i}>
                  <td className="muted" style={{ fontSize: 12 }} title={new Date(e.time).toLocaleString()}>
                    {ago(e.time)}
                  </td>
                  <td>
                    <span className={`badge ${e.kind.includes("crashed") ? "err" : "warn"}`}>
                      {e.kind.replace("instance.", "").replace("key.", "key·")}
                    </span>
                  </td>
                  <td className="mono" style={{ fontSize: 12 }}>{e.instance || e.key || "—"}</td>
                  <td className="mono" style={{ fontSize: 12 }}>{e.model || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
          )}
        </div>
      )}
      {activeJobs.length > 0 && (
        <div className="card">
          <h2>Active jobs</h2>
          <ul>
            {activeJobs.map((j) => (
              <li key={j.id} className="mono" style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <span style={{ minWidth: 220 }}>
                  {j.kind} {j.target} — {Math.round(j.progress * 100)}%
                </span>
                <div className="progress" style={{ flex: 1 }}>
                  <div style={{ width: `${Math.round(j.progress * 100)}%` }} />
                </div>
                {j.message && <span className="muted small">{j.message}</span>}
              </li>
            ))}
          </ul>
        </div>
      )}
      {completedJobs.length > 0 && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Recent jobs</h2>
            <div className="row" style={{ gap: 6 }}>
            <button
              className="btn sm"
              aria-pressed={jobStats}
              title="group the job list per kind — CLI: llmctl jobs -stats (F752)"
              onClick={() => setJobStats(!jobStats)}
            >
              {jobStats ? "Show list" : "Stats"}
            </button>
            <button
              className="btn sm"
              title="drop finished jobs from the daemon's memory (F586)"
              onClick={async () => {
                await api.clearJobs();
                refresh();
              }}
            >
              Clear done
            </button>
            <button
              className="btn sm"
              onClick={() => {
                // Same columns as `llmctl jobs --csv` (F433).
                const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                const rows = [
                  "id,kind,target,status,progress,message,error,updated",
                  ...completedJobs.map((j) =>
                    [j.id, j.kind, j.target, j.status, j.progress.toFixed(2),
                      j.message || "", j.error || "", j.updated || j.created || ""]
                      .map(esc)
                      .join(",")
                  ),
                ];
                const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-jobs-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
            <button
              className="btn sm"
              disabled={completedJobs.length === 0}
              title="download the job list as JSON (CLI: llmctl jobs -json)"
              onClick={() => {
                const blob = new Blob([JSON.stringify(completedJobs, null, 2)], { type: "application/json" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-jobs-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export JSON
            </button>
            </div>
          </div>
          {jobStats ? (
            <table className="table" style={{ marginTop: 8 }}>
              <thead>
                <tr>
                  <th>Kind</th>
                  <th>Total</th>
                  <th>Running</th>
                  <th>Done</th>
                  <th>Error</th>
                  <th>Last</th>
                </tr>
              </thead>
              <tbody>
                {(() => {
                  // Per-kind roll-up, same columns as `llmctl jobs -stats` (F752).
                  const m = new Map<string, { total: number; running: number; done: number; error: number; last: string }>();
                  for (const j of jobs) {
                    const r = m.get(j.kind) ?? { total: 0, running: 0, done: 0, error: 0, last: "" };
                    r.total++;
                    if (j.status === "running") r.running++;
                    else if (j.status === "done") r.done++;
                    else if (j.status === "error") r.error++;
                    const t = j.updated || j.created || "";
                    if (t > r.last) r.last = t;
                    m.set(j.kind, r);
                  }
                  return [...m.entries()]
                    .sort((a, b) => b[1].total - a[1].total)
                    .map(([kind, r]) => (
                      <tr key={kind}>
                        <td className="mono">{kind}</td>
                        <td className="oc-num">{r.total}</td>
                        <td className="oc-num">{r.running}</td>
                        <td className="oc-num">{r.done}</td>
                        <td className="oc-num">{r.error}</td>
                        <td className="muted" style={{ fontSize: 12 }}>{r.last ? ago(r.last) : "—"}</td>
                      </tr>
                    ));
                })()}
              </tbody>
            </table>
          ) : (
          <ul>
            {completedJobs.map((j) => (
              <li key={j.id} className="mono">
                {j.kind} {j.target} — {Math.round(j.progress * 100)}%{j.status === "error" && ` (error: ${j.error})`}
              </li>
            ))}
          </ul>
          )}
        </div>
      )}
      <div className="card">
        <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Daemon log</h2>
          <div className="row" style={{ gap: 6 }}>
          <input
            className="oc-input"
            style={{ width: 140 }}
            placeholder="filter lines"
            title="show only log lines containing this text (F450)"
            value={daemonLogFilter}
            onChange={(e) => setDaemonLogFilter(e.target.value)}
            disabled={daemonLog === null}
          />
          <button
            className={`btn sm${daemonFollow ? " primary" : ""}`}
            title="keep tailing the daemon log every 2s (CLI: llmctl logs -daemon -f)"
            onClick={() => setDaemonFollow((v) => !v)}
            disabled={daemonLog === null}
          >
            {daemonFollow ? "Following" : "Follow"}
          </button>
          <button
            className="btn sm"
            disabled={daemonLog === null}
            title="download the displayed daemon log tail as a text file (twin of llmctl logs -out)"
            onClick={() => {
              const f = daemonLogFilter.trim();
              const text = f && daemonLog ? daemonLog.split("\n").filter((l) => l.toLowerCase().includes(f.toLowerCase())).join("\n") : daemonLog || "";
              const blob = new Blob([text + "\n"], { type: "text/plain" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-daemon-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.log`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Download
          </button>
          <button
            className="btn sm"
            onClick={async () => {
              try {
                if (daemonLog !== null) {
                  setDaemonFollow(false);
                  return setDaemonLog(null);
                }
                const r = await api.daemonLog();
                setDaemonLog(r.logs);
              } catch (e) {
                setDaemonLog(`error: ${e instanceof Error ? e.message : e}`);
              }
            }}
          >
            {daemonLog === null ? "Show" : "Hide"}
          </button>
          </div>
        </div>
        {daemonLog !== null && (
          <pre
            ref={daemonPreRef}
            style={{
              margin: "10px 0 0",
              fontSize: 11,
              maxHeight: 300,
              overflow: "auto",
              background: "var(--code-background, rgba(127,127,127,0.08))",
              padding: 8,
              whiteSpace: "pre-wrap",
            }}
          >
            {(() => {
              // Optional substring filter over the daemon log (F450).
              const f = daemonLogFilter.trim();
              if (!f || daemonLog === null) return daemonLog;
              const lines = daemonLog.split("\n").filter((l) => l.toLowerCase().includes(f.toLowerCase()));
              return lines.length ? lines.join("\n") : `no lines match “${f}”`;
            })()}
          </pre>
        )}
      </div>
    </>
  );
}

function Stat({ label, value, to }: { label: string; value: string | number; to?: string }) {
  return (
    <div>
      <div className="muted" style={{ fontSize: 11, textTransform: "uppercase", letterSpacing: 0.5 }}>
        {label}
      </div>
      <div className="mono" style={{ fontSize: 18 }}>
        {to ? (
          <Link to={to} title="open the Requests page filtered to this time window (F747)">
            {value}
          </Link>
        ) : (
          value
        )}
      </div>
    </div>
  );
}

function fmtMs(v?: number): string {
  return v === undefined || v === null ? "—" : `${v.toFixed(1)}ms`;
}

function fmtBytes(n: number): string {
  if (!n) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}
