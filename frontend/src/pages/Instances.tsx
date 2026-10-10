import { useState } from "react";
import { api } from "../api/client";
import { useStore } from "../state/store";
import { ok, err } from "../ui/toast";

export default function Instances() {
  const { instances, models, backends, refreshOne } = useStore();
  const [model, setModel] = useState("");
  const [backend, setBackend] = useState("llama.cpp");
  const [port, setPort] = useState("");
  const [vars, setVars] = useState("");
  const [busy, setBusy] = useState<string | null>(null);

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
      ok(`started ${model}`);
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
          <label>Extra vars (JSON, optional — e.g. {"{"}num_ctx: 8192{"}"})</label>
          <input className="mono" style={{ width: "100%" }} value={vars} onChange={(e) => setVars(e.target.value)} />
        </div>
      </div>
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
    </>
  );
}
