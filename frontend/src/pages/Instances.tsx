import { useEffect, useState } from "react";
import { api, type AutoStartSpec, type InstanceLogTail } from "../api/client";
import { useStore } from "../state/store";
import { ok, err } from "../ui/toast";

export default function Instances() {
  const { instances, models, backends, refreshOne } = useStore();
  const [model, setModel] = useState("");
  const [backend, setBackend] = useState("llama.cpp");
  const [port, setPort] = useState("");
  const [vars, setVars] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [logFor, setLogFor] = useState<string | null>(null);
  const [logStream, setLogStream] = useState<"stdout" | "stderr">("stderr");
  const [logData, setLogData] = useState<InstanceLogTail | null>(null);
  const [logBusy, setLogBusy] = useState(false);
  const [autoStart, setAutoStart] = useState(false);
  const [autoSpecs, setAutoSpecs] = useState<AutoStartSpec[]>([]);

  useEffect(() => {
    api.autoStart().then(setAutoSpecs).catch(() => setAutoSpecs([]));
  }, []);

  const loadLogs = async (id: string, stream: "stdout" | "stderr") => {
    setLogBusy(true);
    try {
      setLogData(await api.instanceLogs(id, stream));
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
      setLogData(null);
    } finally {
      setLogBusy(false);
    }
  };

  const openLogs = (id: string) => {
    setLogFor(id);
    setLogStream("stderr");
    void loadLogs(id, "stderr");
  };

  const switchStream = (stream: "stdout" | "stderr") => {
    setLogStream(stream);
    if (logFor) void loadLogs(logFor, stream);
  };

  const start = async () => {
    if (!model) return;
    setBusy("start");
    try {
      let v: Record<string, string> | undefined;
      if (vars.trim()) {
        const parsed = JSON.parse(vars);
        v = parsed;
      }
      await api.startInstance({
        model,
        backend,
        port: port ? Number(port) : undefined,
        vars: v,
      });
      if (autoStart) {
        await api.setAutoStart({
          name: "",
          modelId: model,
          backendId: backend,
          port: port ? Number(port) : 0,
          vars: v,
        });
      }
      ok(`started ${model}${autoStart ? " (auto-start enabled)" : ""}`);
      await refreshOne("instances");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const stop = async (id: string) => {
    setBusy(id);
    try {
      await api.stopInstance(id);
      ok(`stopped ${id}`);
      await refreshOne("instances");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const readyModels = models.filter((m) => m.downloaded);

  return (
    <>
      <div className="card">
        <h2>Start an instance</h2>
        <div className="row">
          <div style={{ flex: 1 }}>
            <label>Model</label>
            <select value={model} onChange={(e) => setModel(e.target.value)}>
              <option value="">— select —</option>
              {readyModels.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.id}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label>Backend</label>
            <select value={backend} onChange={(e) => setBackend(e.target.value)}>
              {backends.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.id}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label>Port (0 = auto)</label>
            <input style={{ width: 110 }} value={port} onChange={(e) => setPort(e.target.value)} placeholder="0" />
          </div>
          <div style={{ alignSelf: "flex-end" }}>
            <button className="btn primary" disabled={busy === "start" || !model} onClick={start}>
              {busy === "start" ? "Starting…" : "Start"}
            </button>
          </div>
        </div>
        <div style={{ marginTop: 10 }}>
          <label>{`Extra vars (JSON, optional — e.g. {"ctx": 8192} for context size; empty uses the model's default)`}</label>
          <input className="mono" style={{ width: "100%" }} value={vars} onChange={(e) => setVars(e.target.value)} />
        </div>
        <label style={{ display: "inline-flex", gap: 6, alignItems: "center", marginTop: 10 }}>
          <input
            type="checkbox"
            checked={autoStart}
            onChange={(e) => setAutoStart(e.target.checked)}
          />
          Auto-start this model when the daemon starts
        </label>
      </div>
      {autoSpecs.length > 0 && (
        <div className="card">
          <h2>Auto-start entries</h2>
          <table>
            <thead>
              <tr>
                <th>Model</th>
                <th>Backend</th>
                <th>Port</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {autoSpecs.map((s) => (
                <tr key={s.modelId}>
                  <td className="mono">{s.modelId}</td>
                  <td className="mono">{s.backendId || "—"}</td>
                  <td>{s.port || "auto"}</td>
                  <td style={{ textAlign: "right" }}>
                    <button
                      className="btn sm danger"
                      onClick={async () => {
                        try {
                          await api.clearAutoStart(s.modelId);
                          setAutoSpecs((cur) => cur.filter((x) => x.modelId !== s.modelId));
                          ok(`auto-start removed for ${s.modelId}`);
                        } catch (e) {
                          err(e instanceof Error ? e.message : String(e));
                        }
                      }}
                    >
                      Remove
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className="card">
        <h2>Instances</h2>
        {instances.length ? (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Model</th>
                <th>Backend</th>
                <th>Transport</th>
                <th>Port</th>
                <th>Status</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {instances.map((i) => (
                <tr key={i.id}>
                  <td className="mono">{i.id}</td>
                  <td className="mono">{i.modelId}</td>
                  <td>{i.backendId}</td>
                  <td>{i.transport}</td>
                  <td>{i.port || "—"}</td>
                  <td>
                    {i.status === "running" ? (
                      <span className="badge run">running</span>
                    ) : i.status === "starting" ? (
                      <span className="badge warn">starting</span>
                    ) : i.status === "error" ? (
                      <span className="badge err" title={i.lastError}>error</span>
                    ) : (
                      <span className="badge">{i.status}</span>
                    )}
                  </td>
                  <td style={{ textAlign: "right", whiteSpace: "nowrap" }}>
                    <button className="btn sm" style={{ marginRight: 6 }} onClick={() => openLogs(i.id)}>
                      Logs
                    </button>
                    {i.status === "running" || i.status === "starting" ? (
                      <button
                        className="btn sm danger"
                        disabled={busy === i.id}
                        onClick={() => stop(i.id)}
                      >
                        Stop
                      </button>
                    ) : (
                      <button className="btn sm" disabled={busy === i.id} onClick={() => stop(i.id)}>
                        Clear
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <div className="empty">no instances</div>
        )}
      </div>
      {logFor && (
        <div className="card">
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>
              Logs — <span className="mono">{logFor}</span>{" "}
              {logData && (
                <span style={{ fontWeight: 400, color: "var(--muted-foreground, #888)", fontSize: 12 }}>
                  ({logData.truncated ? `tail of ${logData.size} bytes` : `${logData.size} bytes`})
                </span>
              )}
            </h2>
            <div className="row" style={{ gap: 8 }}>
              <button
                className={`btn sm${logStream === "stderr" ? " primary" : ""}`}
                onClick={() => switchStream("stderr")}
              >
                stderr
              </button>
              <button
                className={`btn sm${logStream === "stdout" ? " primary" : ""}`}
                onClick={() => switchStream("stdout")}
              >
                stdout
              </button>
              <button className="btn sm" disabled={logBusy} onClick={() => void loadLogs(logFor, logStream)}>
                {logBusy ? "Loading…" : "Refresh"}
              </button>
              <button className="btn sm" onClick={() => { setLogFor(null); setLogData(null); }}>
                Close
              </button>
            </div>
          </div>
          <pre
            className="mono"
            style={{
              marginTop: 10,
              maxHeight: 360,
              overflow: "auto",
              background: "rgba(0,0,0,0.35)",
              padding: 10,
              borderRadius: 6,
              whiteSpace: "pre-wrap",
              wordBreak: "break-all",
              fontSize: 12,
            }}
          >
            {logBusy && !logData ? "loading…" : logData?.logs || "(empty)"}
          </pre>
        </div>
      )}
    </>
  );
}
