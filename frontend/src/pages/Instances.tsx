import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, type AutoStartSpec, type PresetSpec, type InstanceLogTail } from "../api/client";
import { useStore } from "../state/store";
import { ok, err } from "../ui/toast";

function uptime(startedAt: string): string {
  const s = Math.max(0, Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`;
}

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
  const [varsFor, setVarsFor] = useState<string | null>(null);
  const [varsText, setVarsText] = useState("");
  const [varsCap, setVarsCap] = useState(0);
  const [varsNote, setVarsNote] = useState("");
  const [varsBusy, setVarsBusy] = useState(false);
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

  const openVars = (id: string, vars?: Record<string, string>, cap?: number, note?: string) => {
    setVarsFor(id);
    setVarsText(JSON.stringify(vars ?? {}, null, 2));
    setVarsCap(cap ?? 0);
    setVarsNote(note ?? "");
  };

  const saveVars = async () => {
    if (!varsFor) return;
    let parsed: Record<string, string>;
    try {
      parsed = JSON.parse(varsText) as Record<string, string>;
    } catch {
      err("vars must be valid JSON, e.g. {\"ctx\": \"8192\"}");
      return;
    }
    setVarsBusy(true);
    try {
      const r = await api.setInstanceVars(varsFor, parsed);
      await api.setInstanceCap(varsFor, varsCap);
      await api.setInstanceNote(varsFor, varsNote.trim());
      ok(r.restartRequired ? "vars saved — restart to apply" : "vars saved");
      setVarsFor(null);
      await refreshOne("instances");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setVarsBusy(false);
    }
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

  const restart = async (id: string) => {
    setBusy(id);
    try {
      await api.restartInstance(id);
      ok(`restarted ${id}`);
      await refreshOne("instances");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const toggleAuto = async (id: string, enabled: boolean) => {
    setBusy(id);
    try {
      await api.setAutoRestart(id, enabled);
      ok(`${id}: auto-restart ${enabled ? "on" : "off"}`);
      await refreshOne("instances");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const stopAll = async () => {
    setBusy("stop-all");
    try {
      const r = await api.stopAll();
      ok(r.stopped.length ? `stopped ${r.stopped.length} instance(s)` : "nothing to stop");
      await refreshOne("instances");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const readyModels = models.filter((m) => m.downloaded);

  // ---- saved instance configs (presets) ----
  const [presets, setPresets] = useState<PresetSpec[]>([]);
  const [presetName, setPresetName] = useState("");
  const loadPresets = () => api.presets().then(setPresets).catch(() => setPresets([]));
  useEffect(() => {
    loadPresets();
  }, []);

  const saveConfig = async () => {
    if (!model) {
      err("select a model first");
      return;
    }
    const name = presetName.trim();
    if (!name) {
      err("give the config a name first");
      return;
    }
    let v: Record<string, string> | undefined;
    if (vars.trim()) {
      try {
        v = JSON.parse(vars);
      } catch {
        err("vars must be valid JSON");
        return;
      }
    }
    setBusy("preset");
    try {
      await api.savePreset({ name, modelId: model, backendId: backend, port: port ? Number(port) : 0, vars: v });
      ok(`saved config "${name}"`);
      setPresetName("");
      await loadPresets();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const startPreset = async (name: string) => {
    setBusy(`p:${name}`);
    try {
      const inst = await api.startPreset(name);
      ok(`started ${inst.id}`);
      await refreshOne("instances");
      await loadPresets();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const deletePreset = async (name: string) => {
    setBusy(`p:${name}`);
    try {
      await api.deletePreset(name);
      ok(`deleted config "${name}"`);
      await loadPresets();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const loadPresetIntoForm = (p: PresetSpec) => {
    setModel(p.modelId);
    if (p.backendId) setBackend(p.backendId);
    setPort(p.port ? String(p.port) : "");
    setVars(p.vars && Object.keys(p.vars).length ? JSON.stringify(p.vars) : "");
  };

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
        <div className="row" style={{ marginTop: 12, alignItems: "flex-end" }}>
          <div style={{ flex: 1 }}>
            <label>Save this configuration</label>
            <input style={{ width: "100%" }} placeholder='name it, e.g. "qwen big-ctx"' value={presetName} onChange={(e) => setPresetName(e.target.value)} />
          </div>
          <button className="btn" disabled={busy === "preset" || !model || !presetName.trim()} onClick={saveConfig}>
            {busy === "preset" ? "Saving…" : "Save config"}
          </button>
        </div>
      </div>
      {presets.length > 0 && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Saved configs</h2>
            <button
              className="btn sm"
              onClick={() => {
                // Same columns as `llmctl presets list --csv` (F399).
                const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                const varsStr = (v?: Record<string, string>) =>
                  Object.entries(v ?? {}).sort().map(([k, val]) => `${k}=${val}`).join(";");
                const rows = [
                  "name,model,instance,backend,port,vars",
                  ...presets.map((p) =>
                    [p.name, p.modelId, p.instance || "", p.backendId || "", String(p.port ?? 0), varsStr(p.vars)]
                      .map(esc)
                      .join(",")
                  ),
                ];
                const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-presets-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
          </div>
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Model</th>
                <th>Backend</th>
                <th>Port</th>
                <th>Vars</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {presets.map((p) => (
                <tr key={p.name}>
                  <td>{p.name}</td>
                  <td className="mono">{p.modelId}</td>
                  <td>{p.backendId || "auto"}</td>
                  <td>{p.port || "auto"}</td>
                  <td className="mono muted">{p.vars && Object.keys(p.vars).length ? JSON.stringify(p.vars) : "—"}</td>
                  <td style={{ textAlign: "right", whiteSpace: "nowrap" }}>
                    <button className="btn sm" onClick={() => loadPresetIntoForm(p)}>Edit</button>{" "}
                    <button className="btn sm primary" disabled={busy === `p:${p.name}`} onClick={() => startPreset(p.name)}>
                      Start
                    </button>{" "}
                    <button className="btn sm danger" disabled={busy === `p:${p.name}`} onClick={() => deletePreset(p.name)}>
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {autoSpecs.length > 0 && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Auto-start entries</h2>
            <button
              className="btn sm"
              onClick={() => {
                // Same columns as `llmctl autostart list --csv` (F400).
                const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                const varsStr = (v?: Record<string, string>) =>
                  Object.entries(v ?? {}).sort().map(([k, val]) => `${k}=${val}`).join(";");
                const rows = [
                  "name,model,backend,port,vars",
                  ...autoSpecs.map((s) =>
                    [s.name || "", s.modelId, s.backendId || "", String(s.port ?? ""), varsStr(s.vars)]
                      .map(esc)
                      .join(",")
                  ),
                ];
                const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-autostart-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
          </div>
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
        <div className="row" style={{ alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Instances</h2>
          <div className="spacer" />
          {instances.some((i) => i.status === "running" && !i.attached) && (
            <button className="btn sm danger" disabled={busy === "stop-all"} onClick={stopAll}>
              {busy === "stop-all" ? "Stopping…" : "Stop all"}
            </button>
          )}
        </div>
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
                <th>Uptime</th>
                <th>Last used</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {instances.map((i) => (
                <tr key={i.id}>
                  <td className="mono">
                    {i.id}
                    {i.note ? (
                      <div className="muted" style={{ fontSize: 11, maxWidth: 220 }} title={i.note}>
                        {i.note}
                      </div>
                    ) : null}
                  </td>
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
                    {(i.inflight ?? 0) > 0 ? (
                      <span className="badge" title="requests in flight">
                        {i.inflight} busy
                      </span>
                    ) : null}
                    {(i.restartCount ?? 0) > 0 ? (
                      <span className="badge warn" title={`${i.restartCount} auto-restart(s) since creation`}>
                        ↻ {i.restartCount}
                      </span>
                    ) : null}
                  </td>
                  <td className="muted" style={{ fontSize: 12 }}>
                    {i.status === "running" && i.startedAt ? uptime(i.startedAt) : "—"}
                  </td>
                  <td className="muted" style={{ fontSize: 12 }} title={i.lastUsed ? new Date(i.lastUsed).toLocaleString() : ""}>
                    {i.lastUsed ? `idle ${uptime(i.lastUsed)}` : "—"}
                  </td>
                  <td style={{ textAlign: "right", whiteSpace: "nowrap" }}>
                    <button className="btn sm" style={{ marginRight: 6 }} onClick={() => openLogs(i.id)}>
                      Logs
                    </button>
                    <Link to={`/chat?model=${encodeURIComponent(i.modelId)}`}>
                      <button className="btn sm" style={{ marginRight: 6 }} title="open the Chat page with this model">
                        Chat
                      </button>
                    </Link>
                    <Link to={`/requests?instance=${encodeURIComponent(i.id)}`}>
                      <button className="btn sm" style={{ marginRight: 6 }} title="gateway requests served by this instance">
                        Req{i.served ? ` ${i.served}` : ""}
                      </button>
                    </Link>
                    <button className="btn sm" style={{ marginRight: 6 }} onClick={() => openVars(i.id, i.vars, i.maxParallel, i.note)}>
                      Vars
                    </button>
                    {!i.attached && (
                      <button
                        className={`btn sm ${i.autoRestart ? "primary" : ""}`}
                        style={{ marginRight: 6 }}
                        disabled={busy === i.id}
                        title="Relaunch automatically when the process crashes (max 3 per 5 min)"
                        onClick={() => toggleAuto(i.id, !i.autoRestart)}
                      >
                        Auto
                      </button>
                    )}
                    {i.status === "running" || i.status === "starting" ? (
                      <>
                        {i.status === "running" && !i.attached && (
                          <button
                            className="btn sm"
                            style={{ marginRight: 6 }}
                            disabled={busy === i.id}
                            onClick={() => restart(i.id)}
                          >
                            Restart
                          </button>
                        )}
                        <button
                          className="btn sm danger"
                          disabled={busy === i.id}
                          onClick={() => stop(i.id)}
                        >
                          Stop
                        </button>
                      </>
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
              <a
                className="btn sm"
                href={`/api/v1/instances/${encodeURIComponent(logFor)}/logs/download?stream=${logStream}`}
                title="Download full log"
              >
                Download
              </a>
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
      {varsFor && (
        <div className="card">
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>
              Vars — <span className="mono">{varsFor}</span>
            </h2>
            <div className="row" style={{ gap: 8 }}>
              <button className="btn sm primary" disabled={varsBusy} onClick={saveVars}>
                {varsBusy ? "Saving…" : "Save"}
              </button>
              <button className="btn sm" onClick={() => setVarsFor(null)}>
                Close
              </button>
            </div>
          </div>
          <p className="muted small" style={{ margin: "6px 0" }}>
            JSON object of start parameters (ctx, ngl, url, …). A running
            instance applies them on next restart.
          </p>
          <textarea
            className="mono"
            style={{ width: "100%", minHeight: 140, background: "rgba(0,0,0,0.25)", color: "inherit", borderRadius: 6, padding: 10 }}
            value={varsText}
            onChange={(e) => setVarsText(e.target.value)}
          />
          <div className="row" style={{ marginTop: 8, alignItems: "center", gap: 8 }}>
            <span className="muted small">max parallel</span>
            <input
              type="number"
              min={0}
              style={{ width: 90 }}
              value={varsCap || ""}
              title="Gateway concurrency cap for this instance (0 = unlimited)"
              onChange={(e) => setVarsCap(Math.max(0, Number(e.target.value) || 0))}
            />
            <span className="muted small">0 = unlimited; extra requests go to other replicas or get 503</span>
          </div>
          <div className="row" style={{ marginTop: 8, alignItems: "center", gap: 8 }}>
            <span className="muted small" style={{ width: 90 }}>note</span>
            <input
              style={{ flex: 1 }}
              maxLength={200}
              placeholder="free-text annotation (shown on this row)"
              value={varsNote}
              onChange={(e) => setVarsNote(e.target.value)}
            />
          </div>
        </div>
      )}
    </>
  );
}
