import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { api, type AutoStartSpec, type PresetSpec, type InstanceLogTail, type Instance } from "../api/client";
import { useStore } from "../state/store";
import { ok, err } from "../ui/toast";

function uptime(startedAt: string): string {
  const s = Math.max(0, Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`;
}

function downloadJson(data: unknown, prefix: string) {
  const blob = new Blob([JSON.stringify(data, null, 2) + "\n"], { type: "application/json" });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = `${prefix}-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
  a.click();
  URL.revokeObjectURL(a.href);
}

async function readJsonArray(f: File): Promise<Array<Record<string, unknown>>> {
  const list = JSON.parse(await f.text());
  if (!Array.isArray(list)) throw new Error("expected a JSON array");
  return list;
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
  const [logFollow, setLogFollow] = useState(false);
  const [logFilter, setLogFilter] = useState("");
  const [showStats, setShowStats] = useState(false); // per-model rollup (CLI `ps -stats` parity, F635)
  const logPreRef = useRef<HTMLPreElement>(null);
  const [varsFor, setVarsFor] = useState<string | null>(null);
  const [varsText, setVarsText] = useState("");
  const [varsCap, setVarsCap] = useState(0);
  const [varsNote, setVarsNote] = useState("");
  const [varsTags, setVarsTags] = useState("");
  const [varsBusy, setVarsBusy] = useState(false);
  const [autoStart, setAutoStart] = useState(false);
  const [autoSpecs, setAutoSpecs] = useState<AutoStartSpec[]>([]);
  const [autoRefresh, setAutoRefresh] = useState(false); // live-poll the instance table (CLI `ps -watch`, F612)
  const [tagFilter, setTagFilter] = useState(""); // tag filter, CLI `ps -tag` twin (F708)
  const [sortKey, setSortKey] = useState(""); // column sort over the instance list (F776)
  const [sortDir, setSortDir] = useState<1 | -1>(1);

  useEffect(() => {
    api.autoStart().then(setAutoSpecs).catch(() => setAutoSpecs([]));
  }, []);

  const instTags = Array.from(new Set(instances.flatMap((i) => i.tags ?? []))).sort();
  const shownInstances = tagFilter
    ? instances.filter((i) => (i.tags ?? []).includes(tagFilter))
    : instances;
  const toggleSort = (k: string) => {
    if (sortKey === k) setSortDir((d) => (d === 1 ? -1 : 1));
    else {
      setSortKey(k);
      setSortDir(k === "port" ? -1 : 1);
    }
  };
  const sortedInstances = (() => {
    if (!sortKey) return shownInstances;
    const val = (i: Instance): string | number => {
      switch (sortKey) {
        case "id": return i.id || "";
        case "model": return i.modelId || "";
        case "backend": return i.backendId || "";
        case "transport": return i.transport || "";
        case "port": return i.port || 0;
        case "status": return i.status || "";
        case "uptime": return i.startedAt || "";
        case "lastused": return i.lastUsed || "";
        default: return 0;
      }
    };
    return [...shownInstances].sort((a, b) => {
      const va = val(a);
      const vb = val(b);
      const c = typeof va === "number" && typeof vb === "number" ? va - vb : String(va).localeCompare(String(vb));
      return c * sortDir;
    });
  })();
  const arrow = (k: string) => (sortKey === k ? (sortDir === 1 ? " ▲" : " ▼") : "");

  // Poll the instance list while the toggle is on (F612), so starting/
  // stopping instances and uptime counters update without a reload.
  useEffect(() => {
    if (!autoRefresh) return;
    const t = setInterval(() => refreshOne("instances"), 5000);
    return () => clearInterval(t);
  }, [autoRefresh, refreshOne]);

  // JSON export/import, same shapes as `llmctl presets|autostart export|import`
  // (F413, F414).
  const presetImportRef = useRef<HTMLInputElement>(null);
  const autoImportRef = useRef<HTMLInputElement>(null);
  const importPresets = async (f: File) => {
    try {
      const list = await readJsonArray(f);
      let created = 0;
      let skipped = 0;
      for (const p of list) {
        try {
          await api.savePreset(p as unknown as PresetSpec);
          created++;
        } catch {
          skipped++;
        }
      }
      ok(`imported ${created} presets (${skipped} skipped)`);
      loadPresets();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };
  const importAutos = async (f: File) => {
    try {
      const list = await readJsonArray(f);
      let created = 0;
      let skipped = 0;
      for (const s of list) {
        try {
          await api.setAutoStart(s as unknown as AutoStartSpec);
          created++;
        } catch {
          skipped++;
        }
      }
      ok(`imported ${created} auto-start entries (${skipped} skipped)`);
      api.autoStart().then(setAutoSpecs).catch(() => {});
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const loadLogs = async (id: string, stream: "stdout" | "stderr", silent = false) => {
    if (!silent) setLogBusy(true);
    try {
      setLogData(await api.instanceLogs(id, stream));
    } catch (e) {
      if (!silent) err(e instanceof Error ? e.message : String(e));
      if (!silent) setLogData(null);
    } finally {
      if (!silent) setLogBusy(false);
    }
  };

  // Follow mode: poll the log tail every 2s, like `llmctl logs -f` (F422).
  useEffect(() => {
    if (!logFollow || !logFor) return;
    const t = setInterval(() => void loadLogs(logFor, logStream, true), 2000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [logFollow, logFor, logStream]);

  // Keep the newest lines visible while following.
  useEffect(() => {
    if (logFollow && logPreRef.current) {
      logPreRef.current.scrollTop = logPreRef.current.scrollHeight;
    }
  }, [logData, logFollow]);

  const openLogs = (id: string) => {
    setLogFor(id);
    setLogStream("stderr");
    setLogFollow(false);
    setLogFilter("");
    void loadLogs(id, "stderr");
  };

  const openVars = (id: string, vars?: Record<string, string>, cap?: number, note?: string, tags?: string[]) => {
    setVarsFor(id);
    setVarsText(JSON.stringify(vars ?? {}, null, 2));
    setVarsCap(cap ?? 0);
    setVarsNote(note ?? "");
    setVarsTags((tags ?? []).join(", "));
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
      await api.setInstanceTags(
        varsFor,
        varsTags.split(",").map((t) => t.trim()).filter(Boolean)
      );
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

  // Restart every non-attached running instance (UI twin of `llmctl restart --all`, F446).
  const restartAll = async () => {
    setBusy("restart-all");
    try {
      const list = await api.instances();
      const targets = list.filter((i) => i.status === "running" && !i.attached);
      let okCount = 0;
      let failed = 0;
      for (const i of targets) {
        try {
          await api.restartInstance(i.id);
          okCount++;
        } catch {
          failed++;
        }
      }
      ok(failed ? `restarted ${okCount}, failed ${failed}` : `restarted ${okCount} instance(s)`);
      await refreshOne("instances");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const stopAll = async () => {    setBusy("stop-all");
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

  // Duplicate a running/known instance: start a second one with the same
  // model/backend/vars under a new name (replica helper, F817).
  const duplicateInstance = async (i: Instance) => {
    const name = window.prompt(`Duplicate ${i.id} as (new instance name):`, `${i.id}-copy`);
    if (name === null) return;
    const id = name.trim() || `${i.id}-copy`;
    setBusy(`dup:${i.id}`);
    try {
      await api.startInstance({ model: i.modelId, backend: i.backendId, instance: id, vars: i.vars });
      ok(`instance ${id} starting`);
      await refreshOne("instances");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy("");
    }
  };

  // Duplicate a preset under a new name — UI twin of `llmctl presets duplicate` (F649).
  const duplicatePreset = async (p: PresetSpec) => {
    const name = window.prompt("Name for the copy:", `${p.name} copy`);
    if (name === null) return;
    const finalName = name.trim() || `${p.name} copy`;
    setBusy(`p:${p.name}`);
    try {
      await api.savePreset({ ...p, name: finalName });
      ok(`preset "${finalName}" created`);
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
            <div className="row" style={{ gap: 6 }}>
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
            <button
              className="btn sm"
              title="download presets as JSON (CLI: llmctl presets export)"
              onClick={() => downloadJson(presets, "llmctl-presets")}
            >
              Export JSON
            </button>
            <button
              className="btn sm"
              title="import a presets JSON file (CLI: llmctl presets import)"
              onClick={() => presetImportRef.current?.click()}
            >
              Import JSON
            </button>
            <input
              ref={presetImportRef}
              type="file"
              accept="application/json,.json"
              style={{ display: "none" }}
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) importPresets(f);
                e.target.value = "";
              }}
            />
            </div>
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
                    <button className="btn sm" title="duplicate this preset under a new name (CLI: llmctl presets duplicate)" onClick={() => duplicatePreset(p)}>Duplicate</button>{" "}
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
            <div className="row" style={{ gap: 6 }}>
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
            <button
              className="btn sm"
              title="download entries as JSON (CLI: llmctl autostart export)"
              onClick={() => downloadJson(autoSpecs, "llmctl-autostart")}
            >
              Export JSON
            </button>
            <button
              className="btn sm"
              title="import an autostart JSON file (CLI: llmctl autostart import)"
              onClick={() => autoImportRef.current?.click()}
            >
              Import JSON
            </button>
            <input
              ref={autoImportRef}
              type="file"
              accept="application/json,.json"
              style={{ display: "none" }}
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) importAutos(f);
                e.target.value = "";
              }}
            />
            </div>
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
          <label
            style={{ display: "inline-flex", gap: 6, alignItems: "center", fontSize: 12 }}
            title="poll every 5s so status and uptime update without a reload (CLI: llmctl ps -watch N, F612)"
          >
            <input type="checkbox" checked={autoRefresh} onChange={(e) => setAutoRefresh(e.target.checked)} />
            Auto-refresh
          </label>
          <button
            className="btn sm"
            disabled={instances.length === 0}
            onClick={() => {
              // Same columns as `llmctl ps --csv` (F406).
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "id,model,backend,transport,port,status,uptime_s,served,restarts,note,tags",
                ...instances.map((i) => {
                  const up = i.startedAt ? Math.max(0, Math.floor((Date.now() - new Date(i.startedAt).getTime()) / 1000)) : 0;
                  return [
                    i.id, i.modelId, i.backendId || "", i.transport, String(i.port || ""),
                    i.status, String(i.status === "running" ? up : 0), String(i.served ?? 0),
                    String(i.restartCount ?? 0), i.note || "", (i.tags ?? []).join(";"),
                  ]
                    .map(esc)
                    .join(",");
                }),
              ];
              const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-ps-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export CSV
          </button>
          <button
            className="btn sm"
            disabled={shownInstances.length === 0}
            title="download the instance list as JSON (CLI: llmctl ps -json)"
            onClick={() => {
              const blob = new Blob([JSON.stringify(shownInstances, null, 2)], { type: "application/json" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-ps-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export JSON
          </button>
          <button
            className="btn sm"
            aria-pressed={showStats}
            disabled={instances.length === 0}
            title="group instances per model — CLI: llmctl ps -stats (F635)"
            onClick={() => setShowStats(!showStats)}
          >
            {showStats ? "Show list" : "Stats"}
          </button>
          {instTags.length > 0 && (
            <select
              className="oc-input"
              style={{ width: "auto", fontSize: 12 }}
              title="filter by tag — CLI: llmctl ps -tag (F708)"
              value={tagFilter}
              onChange={(e) => setTagFilter(e.target.value)}
            >
              <option value="">all tags</option>
              {instTags.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          )}
          {instances.some((i) => i.status === "running" && !i.attached) && (
            <>
              <button className="btn sm" disabled={busy === "restart-all"} onClick={restartAll}>
                {busy === "restart-all" ? "Restarting…" : "Restart all"}
              </button>
              <button className="btn sm danger" disabled={busy === "stop-all"} onClick={stopAll}>
                {busy === "stop-all" ? "Stopping…" : "Stop all"}
              </button>
            </>
          )}
        </div>
        {instances.length ? (
          showStats ? (
          <table>
            <thead>
              <tr>
                <th>Model</th>
                <th>Replicas</th>
                <th>Running</th>
                <th>Served</th>
                <th>Restarts</th>
              </tr>
            </thead>
            <tbody>
              {(() => {
                // Same rollup as `llmctl ps -stats` (F635): one row per model,
                // sorted by requests served.
                const by = new Map<string, { replicas: number; running: number; served: number; restarts: number }>();
                for (const i of instances) {
                  const g = by.get(i.modelId) ?? { replicas: 0, running: 0, served: 0, restarts: 0 };
                  g.replicas++;
                  if (i.status === "running") g.running++;
                  g.served += i.served ?? 0;
                  g.restarts += i.restartCount ?? 0;
                  by.set(i.modelId, g);
                }
                return [...by.entries()]
                  .sort((a, b) => b[1].served - a[1].served)
                  .map(([model, g]) => (
                    <tr key={model}>
                      <td>{model}</td>
                      <td>{g.replicas}</td>
                      <td>{g.running}</td>
                      <td>{g.served}</td>
                      <td>{g.restarts}</td>
                    </tr>
                  ));
              })()}
            </tbody>
          </table>
          ) : (
          <table>
            <thead>
              <tr>
                <th style={{ cursor: "pointer" }} title="sort by id (F776)" onClick={() => toggleSort("id")}>ID{arrow("id")}</th>
                <th style={{ cursor: "pointer" }} title="sort by model (F776)" onClick={() => toggleSort("model")}>Model{arrow("model")}</th>
                <th style={{ cursor: "pointer" }} title="sort by backend (F776)" onClick={() => toggleSort("backend")}>Backend{arrow("backend")}</th>
                <th style={{ cursor: "pointer" }} title="sort by transport (F776)" onClick={() => toggleSort("transport")}>Transport{arrow("transport")}</th>
                <th style={{ cursor: "pointer" }} title="sort by port (F776)" onClick={() => toggleSort("port")}>Port{arrow("port")}</th>
                <th style={{ cursor: "pointer" }} title="sort by status (F776)" onClick={() => toggleSort("status")}>Status{arrow("status")}</th>
                <th style={{ cursor: "pointer" }} title="sort by uptime (F776)" onClick={() => toggleSort("uptime")}>Uptime{arrow("uptime")}</th>
                <th style={{ cursor: "pointer" }} title="sort by last used (F776)" onClick={() => toggleSort("lastused")}>Last used{arrow("lastused")}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {sortedInstances.map((i) => (
                <tr key={i.id}>
                  <td className="mono">
                    <span
                      style={{ cursor: "pointer" }}
                      title="click to copy the instance id (llmctl stop <id>, requests -instance)"
                      onClick={() => {
                        if (navigator.clipboard) {
                          void navigator.clipboard.writeText(i.id).then(() => ok(`copied ${i.id}`));
                        }
                      }}
                    >
                      {i.id}
                    </span>
                    {i.note ? (
                      <div className="muted" style={{ fontSize: 11, maxWidth: 220 }} title={i.note}>
                        {i.note}
                      </div>
                    ) : null}
                    {i.tags?.length ? (
                      <div className="muted" style={{ fontSize: 11, maxWidth: 220 }} title={i.tags.join(", ")}>
                        {(i.tags || []).join(", ")}
                      </div>
                    ) : null}
                  </td>
                  <td className="mono">{i.modelId}</td>
                  <td>{i.backendId}</td>
                  <td>{i.transport}</td>
                  <td
                    style={{ cursor: i.port ? "pointer" : "default" }}
                    title={i.port ? "click to copy the direct endpoint URL" : undefined}
                    onClick={() => {
                      if (i.port && navigator.clipboard) {
                        const url = `http://localhost:${i.port}`;
                        void navigator.clipboard.writeText(url).then(() => ok(`copied ${url}`));
                      }
                    }}
                  >
                    {i.port || "—"}
                  </td>
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
                    <button className="btn sm" style={{ marginRight: 6 }} onClick={() => openVars(i.id, i.vars, i.maxParallel, i.note, i.tags)}>
                      Vars
                    </button>
                    <button
                      className="btn sm"
                      style={{ marginRight: 6 }}
                      disabled={busy === `dup:${i.id}`}
                      title="start a second instance with the same model/backend/vars under a new name (F817)"
                      onClick={() => duplicateInstance(i)}
                    >
                      Duplicate
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
          )
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
              <button
                className={`btn sm${logFollow ? " primary" : ""}`}
                title="keep tailing new log lines every 2s (CLI: llmctl logs -f)"
                onClick={() => setLogFollow((v) => !v)}
              >
                {logFollow ? "Following" : "Follow"}
              </button>
              <input
                className="oc-input"
                style={{ width: 140 }}
                placeholder="filter lines"
                title="show only log lines containing this text"
                value={logFilter}
                onChange={(e) => setLogFilter(e.target.value)}
              />
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
              <button
                className="btn sm"
                disabled={logData === null}
                title="copy the displayed log tail to the clipboard (F760)"
                onClick={() => {
                  const raw = logData?.logs || "";
                  const f = logFilter.trim();
                  const text = f ? raw.split("\n").filter((l) => l.toLowerCase().includes(f.toLowerCase())).join("\n") : raw;
                  navigator.clipboard
                    ?.writeText(text)
                    .then(() => ok("copied"))
                    .catch(() => {});
                }}
              >
                Copy
              </button>
              <button className="btn sm" onClick={() => { setLogFor(null); setLogData(null); }}>
                Close
              </button>
            </div>
          </div>
          <pre
            ref={logPreRef}
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
            {(() => {
              // Optional substring filter over the tail (F450).
              const raw = logBusy && !logData ? "loading…" : logData?.logs || "(empty)";
              const f = logFilter.trim();
              if (!f || raw === "loading…") return raw;
              const lines = raw.split("\n").filter((l) => l.toLowerCase().includes(f.toLowerCase()));
              return lines.length ? lines.join("\n") : `no lines match “${f}”`;
            })()}
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
              <button
                className="btn sm"
                title="copy the vars JSON to the clipboard (F791)"
                onClick={() => {
                  navigator.clipboard
                    ?.writeText(varsText)
                    .then(() => ok("copied"))
                    .catch(() => {});
                }}
              >
                Copy
              </button>
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
          <div className="row" style={{ marginTop: 8, alignItems: "center", gap: 8 }}>
            <span className="muted small" style={{ width: 90 }}>tags</span>
            <input
              style={{ flex: 1 }}
              placeholder="comma-separated labels, e.g. primary,gpu (F673)"
              value={varsTags}
              onChange={(e) => setVarsTags(e.target.value)}
            />
          </div>
        </div>
      )}
    </>
  );
}
