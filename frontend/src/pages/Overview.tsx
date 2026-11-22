import { useEffect, useState } from "react";
import { api, type GatewayMetrics, type GPUInfo } from "../api/client";
import { useStore } from "../state/store";

export default function Overview() {
  const { status, instances, jobs } = useStore();
  const [metrics, setMetrics] = useState<GatewayMetrics | null>(null);
  const [gpus, setGpus] = useState<GPUInfo[] | null>(null);
  const [disk, setDisk] = useState<Awaited<ReturnType<typeof api.disk>> | null>(null);
  const [events, setEvents] = useState<Awaited<ReturnType<typeof api.events>>>([]);

  useEffect(() => {
    let alive = true;
    const tick = () => api.events(15).then((e) => alive && setEvents(e)).catch(() => {});
    tick();
    const t = setInterval(tick, 10000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);

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
      <div className="card">
        <h2>GPU</h2>
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
        <h2>Disk</h2>
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
        <h2>Gateway metrics</h2>
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
      {events.length > 0 && (
        <div className="card">
          <h2>Events</h2>
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
                  <td className="muted" style={{ fontSize: 12 }}>
                    {new Date(e.time).toLocaleString()}
                  </td>
                  <td>
                    <span className={`badge ${e.kind.includes("crashed") ? "err" : "warn"}`}>
                      {e.kind.replace("instance.", "")}
                    </span>
                  </td>
                  <td className="mono" style={{ fontSize: 12 }}>{e.instance || "—"}</td>
                  <td className="mono" style={{ fontSize: 12 }}>{e.model || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {activeJobs.length > 0 && (
        <div className="card">
          <h2>Active jobs</h2>
          <ul>
            {activeJobs.map((j) => (
              <li key={j.id} className="mono">
                {j.kind} {j.target} — {Math.round(j.progress * 100)}%
              </li>
            ))}
          </ul>
        </div>
      )}
      {completedJobs.length > 0 && (
        <div className="card">
          <h2>Recent jobs</h2>
          <ul>
            {completedJobs.map((j) => (
              <li key={j.id} className="mono">
                {j.kind} {j.target} — {Math.round(j.progress * 100)}%{j.status === "error" && ` (error: ${j.error})`}
              </li>
            ))}
          </ul>
        </div>
      )}
    </>
  );
}

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <div className="muted" style={{ fontSize: 11, textTransform: "uppercase", letterSpacing: 0.5 }}>
        {label}
      </div>
      <div className="mono" style={{ fontSize: 18 }}>
        {value}
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
