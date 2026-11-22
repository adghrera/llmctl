import { useEffect, useRef, useState } from "react";
import { api, KB, ScheduleSpec, ScheduleRun } from "../api/client";
import { until } from "../lib/time";
import { ok, err } from "../ui/toast";

export default function Scheduler() {
  const [schedules, setSchedules] = useState<ScheduleSpec[]>([]);
  const [name, setName] = useState("");
  const [cronExpr, setCronExpr] = useState("0 9 * * *");
  const [kind, setKind] = useState("chat");
  const [instance, setInstance] = useState("");
  const [keep, setKeep] = useState(7);
  const [model, setModel] = useState("");
  const [prompt, setPrompt] = useState("");
  const [kb, setKb] = useState<string[]>([]);
  const [kbs, setKbs] = useState<KB[]>([]);
  const [webhookUrl, setWebhookUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [runs, setRuns] = useState<ScheduleRun[]>([]);
  const [runTask, setRunTask] = useState(""); // Recent Runs task filter (F688)
  const [runStats, setRunStats] = useState(false); // per-task rollup (CLI `scheduler runs -stats` parity, F602)
  const [autoRuns, setAutoRuns] = useState(false); // live-refresh the run ring (F608)

  const load = () =>
    api
      .schedules()
      .then((list) =>
        // The API returns {id, dir, spec}; flatten spec onto each row and
        // attach the ring's last-run status (F184).
        setSchedules(
          (list as unknown as Array<{ spec?: ScheduleSpec } & ScheduleSpec>).map(
            (r) => ({ ...(r.spec ?? r), lastRun: (r as { lastRun?: ScheduleRun }).lastRun })
          )
        )
      )
      .catch((e) => err(e instanceof Error ? e.message : String(e)));

  useEffect(() => {
    load();
    api.schedulerRuns().then(setRuns).catch(() => setRuns([]));
    api.knowledgeBases().then(setKbs).catch(() => setKbs([]));
  }, []);

  // Live-refresh the Recent Runs ring while the toggle is on (F608),
  // mirroring the Requests page's auto-refresh pattern.
  useEffect(() => {
    if (!autoRuns) return;
    const t = setInterval(() => api.schedulerRuns().then(setRuns).catch(() => {}), 5000);
    return () => clearInterval(t);
  }, [autoRuns]);

  // JSON export/import, same shapes as `llmctl scheduler export|import` (F412).
  const importFileRef = useRef<HTMLInputElement>(null);
  const exportJson = () => {
    const specs = schedules.map((s) => {
      const { last_run: _lr, next_run: _nr, created_at: _c, updated_at: _u, ...spec } = s;
      return spec;
    });
    const blob = new Blob([JSON.stringify(specs, null, 2) + "\n"], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `llmctl-scheduler-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  };
  const importJson = async (f: File) => {
    try {
      const specs = JSON.parse(await f.text()) as Array<Record<string, unknown>>;
      if (!Array.isArray(specs)) throw new Error("expected a JSON array");
      let created = 0;
      let skipped = 0;
      for (const spec of specs) {
        delete spec.id; // the store mints its own ids
        try {
          await api.createSchedule(spec as Parameters<typeof api.createSchedule>[0]);
          created++;
        } catch {
          skipped++;
        }
      }
      ok(`imported ${created} schedules (${skipped} skipped)`);
      load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const create = async () => {
    if (!name.trim() || !cronExpr.trim()) {
      err("name and cron are required");
      return;
    }
    if (kind === "chat" && (!model.trim() || !prompt.trim())) {
      err("chat tasks need a model and a prompt");
      return;
    }
    if (kind === "instance.start" && !model.trim()) {
      err("instance.start needs a model");
      return;
    }
    if (kind === "instance.stop" && !instance.trim() && !model.trim()) {
      err("instance.stop needs an instance id (or model id)");
      return;
    }
    setBusy(true);
    try {
      await api.createSchedule({
        name,
        cron_expr: cronExpr,
        kind,
        model,
        prompt: kind === "chat" ? prompt : "",
        kb: kind === "chat" && kb.length ? kb.join(",") : undefined,
        instance: instance.trim() || undefined,
        keep: kind === "backup" ? keep : undefined,
        webhook_url: webhookUrl.trim() || undefined,
      });
      ok("schedule created");
      setName("");
      setCronExpr("0 9 * * *");
      setModel("");
      setPrompt("");
      setInstance("");
      setKeep(7);
      setWebhookUrl("");
      setKb([]);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    if (!window.confirm("Delete this scheduled task?")) return;
    setBusy(true);
    try {
      await api.deleteSchedule(id);
      ok("schedule deleted");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const editTask = async (s: ScheduleSpec) => {
    const name = window.prompt("Task name:", s.name);
    if (name === null) return;
    const cron = window.prompt("Cron expression:", s.cron_expr);
    if (cron === null) return;
    const model = window.prompt("Model id:", s.model);
    if (model === null) return;
    const prompt =
      (s.kind || "chat") === "chat"
        ? window.prompt("Prompt:", s.prompt)
        : s.prompt;
    if (prompt === null) return;
    setBusy(true);
    try {
      await api.updateSchedule(s.id, {
        name: name.trim() || s.name,
        cron_expr: cron.trim() || s.cron_expr,
        model: model.trim() || s.model,
        prompt,
        enabled: s.enabled,
        webhook_url: s.webhook_url,
      });
      ok("schedule updated");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // free-form tags, comma-separated in the prompt (F681); empty clears.
  const tagTask = async (s: ScheduleSpec) => {
    const next = window.prompt(`Tags for task "${s.name}" (comma-separated, empty clears):`, (s.tags ?? []).join(", "));
    if (next == null) return;
    const tags = next.split(",").map((t) => t.trim()).filter(Boolean);
    try {
      await api.setScheduleTags(s.id, tags);
      ok(tags.length ? `tags set: ${tags.join(", ")}` : "tags cleared");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  // Duplicate a task under a new name — UI twin of `llmctl scheduler duplicate` (F647).
  const dupTask = async (s: ScheduleSpec) => {
    const name = window.prompt("Name for the copy:", `${s.name} copy`);
    if (name === null) return;
    const finalName = name.trim() || `${s.name} copy`;
    setBusy(true);
    try {
      await api.createSchedule({
        name: finalName,
        cron_expr: s.cron_expr,
        kind: s.kind,
        model: s.model,
        prompt: s.prompt,
        instance: s.instance,
        kb: s.kb,
        keep: s.keep,
        webhook_url: s.webhook_url,
        enabled: s.enabled,
      });
      ok(`task "${finalName}" created`);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const toggleEnabled = async (s: ScheduleSpec) => {
    const next = !s.enabled;
    setBusy(true);
    try {
      await api.setScheduleEnabled(s.id, next);
      ok("schedule " + (next ? "enabled" : "disabled"));
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const runNow = async (s: ScheduleSpec) => {
    setBusy(true);
    try {
      await api.runSchedule(s.id);
      ok(`task "${s.name}" started`);
      setTimeout(() => {
        load();
        api.schedulerRuns().then(setRuns).catch(() => {});
      }, 1500);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      <div className="card">
        <h2>Scheduled Tasks</h2>
        <p style={{ opacity: 0.75, margin: "4px 0 10px" }}>
          Schedule tasks with cron expressions: send a prompt to a model
          (result saved as a conversation), or start/stop an instance — e.g.
          load a model every morning, unload it at night.
        </p>
        <div className="row" style={{ gap: 8, marginBottom: 8 }}>
          <input
            style={{ flex: 1, minWidth: 150 }}
            placeholder="Task name"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <input
            style={{ flex: 1, minWidth: 140 }}
            placeholder="Cron expression (e.g. 0 9 * * *)"
            value={cronExpr}
            onChange={(e) => setCronExpr(e.target.value)}
          />
          <select value={kind} onChange={(e) => setKind(e.target.value)} style={{ minWidth: 130 }}>
            <option value="chat">chat prompt</option>
            <option value="instance.start">start instance</option>
            <option value="instance.stop">stop instance</option>
            <option value="backup">backup state</option>
          </select>
          {kind === "backup" && (
            <input
              type="number"
              min={1}
              style={{ width: 110 }}
              placeholder="keep"
              title="Snapshots to retain"
              value={keep || ""}
              onChange={(e) => setKeep(Math.max(1, Number(e.target.value) || 0))}
            />
          )}
          {(kind === "chat" || kind === "instance.start") && (
            <input
              style={{ flex: 1, minWidth: 140 }}
              placeholder={kind === "chat" ? "Model id" : "Model id to start"}
              value={model}
              onChange={(e) => setModel(e.target.value)}
            />
          )}
          {kind === "instance.stop" && (
            <input
              style={{ flex: 1, minWidth: 140 }}
              placeholder="Instance id to stop"
              value={instance}
              onChange={(e) => setInstance(e.target.value)}
            />
          )}
          {kind === "chat" && (
            <input
              style={{ flex: 2, minWidth: 200 }}
              placeholder="Prompt to send"
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
            />
          )}
          {kind === "chat" && kbs.length > 0 && (
            <select
              style={{ minWidth: 150 }}
              multiple
              size={Math.min(4, kbs.length)}
              title="inject top knowledge-base chunks as context; several KBs merge (F365, F666)"
              value={kb}
              onChange={(e) => setKb(Array.from(e.target.selectedOptions).map((o) => o.value))}
            >
              {kbs.map((k) => (
                <option key={k.id} value={k.id}>
                  kb: {k.kb.name || k.id}
                </option>
              ))}
            </select>
          )}
          <input
            style={{ flex: 2, minWidth: 200 }}
            placeholder="Webhook URL (optional — POSTed result JSON on each run)"
            value={webhookUrl}
            onChange={(e) => setWebhookUrl(e.target.value)}
          />
          <button
            className="btn primary"
            disabled={
              busy ||
              !name.trim() ||
              !cronExpr.trim() ||
              (kind === "chat" && (!model.trim() || !prompt.trim())) ||
              (kind === "instance.start" && !model.trim()) ||
              (kind === "instance.stop" && !instance.trim() && !model.trim())
            }
            onClick={create}
          >
            {busy ? "Adding…" : "Add schedule"}
          </button>
        </div>
      </div>

      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Scheduled Tasks ({schedules.length})</h2>
          <div className="row" style={{ gap: 6 }}>
          <button
            className="btn sm"
            disabled={schedules.length === 0}
            onClick={() => {
              // Same columns as `llmctl scheduler list --csv` (F391).
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "id,name,kind,cron,model,enabled,next_run,last_run_time,last_run_status,tags",
                ...schedules.map((s) =>
                  [
                    s.id, s.name, s.kind || "chat", s.cron_expr, s.model,
                    String(!!s.enabled), s.next_run || "",
                    s.lastRun?.time || (s.last_run?.startsWith("0001") ? "" : s.last_run) || "",
                    s.lastRun?.status || "", (s.tags ?? []).join(";"),
                  ]
                    .map(esc)
                    .join(",")
                ),
              ];
              const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-scheduler-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export CSV
          </button>
          <button
            className="btn sm"
            disabled={schedules.length === 0}
            title="download schedule specs as JSON (CLI: llmctl scheduler export)"
            onClick={exportJson}
          >
            Export JSON
          </button>
          <button
            className="btn sm"
            title="import a scheduler JSON file (CLI: llmctl scheduler import)"
            onClick={() => importFileRef.current?.click()}
          >
            Import JSON
          </button>
          <input
            ref={importFileRef}
            type="file"
            accept="application/json,.json"
            style={{ display: "none" }}
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) importJson(f);
              e.target.value = "";
            }}
          />
          </div>
        </div>
        {schedules.length === 0 && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No scheduled tasks yet.
          </div>
        )}
        {schedules.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Cron</th>
                <th>Kind</th>
                <th>Model</th>
                <th>Hook</th>
                <th>Enabled</th>
                <th>Next Run</th>
                <th>Last Run</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {schedules.map((s) => (
                <tr key={s.id}>
                  <td>
                    {s.name}
                    {s.tags?.length ? (
                      <span className="muted small" title={s.tags.join(", ")}> · {s.tags.join(", ")}</span>
                    ) : null}
                  </td>
                  <td style={{ fontFamily: "monospace", fontSize: 12 }}>
                    {s.cron_expr}
                  </td>
                  <td style={{ fontSize: 12 }}>{s.kind || "chat"}</td>
                  <td style={{ fontFamily: "monospace", fontSize: 12 }}>
                    {s.model}
                    {s.kb ? (
                      <span className="muted small" title={`knowledge base: ${s.kb}`}>
                        {" "}· kb:{s.kb}
                      </span>
                    ) : null}
                  </td>
                  <td style={{ textAlign: "center", fontSize: 12 }}>
                    {s.webhook_url ? (
                      <span title={s.webhook_url} className="badge ok">on</span>
                    ) : (
                      <span className="muted">—</span>
                    )}
                  </td>
                  <td style={{ textAlign: "center" }}>
                    <button className="btn sm" onClick={() => toggleEnabled(s)}>
                      {s.enabled ? "Enabled" : "Disabled"}
                    </button>
                  </td>
                  <td style={{ fontSize: 12, opacity: 0.7 }}>
                    {s.next_run
                      ? <span title={new Date(s.next_run).toLocaleString()}>{until(s.next_run)}</span>
                      : "—"}
                  </td>
                  <td style={{ fontSize: 12, opacity: 0.7 }}>
                    {s.lastRun ? (
                      <span
                        className={`badge ${s.lastRun.status === "ok" ? "ok" : "err"}`}
                        title={s.lastRun.detail || s.lastRun.status}
                        style={{ marginRight: 6 }}
                      >
                        {s.lastRun.status}
                      </span>
                    ) : null}
                    {s.last_run
                      ? new Date(s.last_run).toLocaleString()
                      : "—"}
                  </td>
                  <td style={{ textAlign: "right" }}>
                    <button
                      className="btn sm"
                      style={{ marginRight: 6 }}
                      title="run this task now"
                      onClick={() => runNow(s)}
                    >
                      Run
                    </button>
                    <button
                      className="btn sm"
                      style={{ marginRight: 6 }}
                      title="edit name, cron, model and prompt"
                      onClick={() => editTask(s)}
                    >
                      Edit
                    </button>
                    <button
                      className="btn sm"
                      style={{ marginRight: 6 }}
                      title="duplicate this task under a new name (CLI: llmctl scheduler duplicate)"
                      onClick={() => dupTask(s)}
                    >
                      Duplicate
                    </button>
                    <button
                      className="btn sm"
                      style={{ marginRight: 6 }}
                      title="edit tags (CLI: llmctl scheduler tag)"
                      onClick={() => tagTask(s)}
                    >
                      Tag
                    </button>
                    <button
                      className="btn sm danger"
                      onClick={() => remove(s.id)}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      {runs.length > 0 && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Recent Runs</h2>
            <div className="row" style={{ gap: 6 }}>
            {new Set(runs.map((r) => r.name)).size > 1 && (
              <select
                className="input"
                style={{ fontSize: 12 }}
                title="filter the run log by task (CLI: llmctl scheduler runs -task T, F688)"
                value={runTask}
                onChange={(e) => setRunTask(e.target.value)}
              >
                <option value="">all tasks</option>
                {Array.from(new Set(runs.map((r) => r.name))).map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            )}
            <label style={{ display: "inline-flex", gap: 6, alignItems: "center", fontSize: 12 }} title="poll every 5s while a task fires (F608)">
              <input type="checkbox" checked={autoRuns} onChange={(e) => setAutoRuns(e.target.checked)} />
              Auto-refresh
            </label>
            <button
              className="btn sm"
              aria-pressed={runStats}
              title="group the run log per task — CLI: llmctl scheduler runs -stats (F602)"
              onClick={() => setRunStats(!runStats)}
            >
              {runStats ? "Show list" : "Stats"}
            </button>
            <button
              className="btn sm"
              onClick={() => {
                // Same columns as `llmctl scheduler runs --csv` (F419).
                const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                const rows = [
                  "time,task_id,name,kind,status,duration_ms,detail",
                  ...(runTask ? runs.filter((r) => r.name === runTask) : runs).map((r) =>
                    [r.time, r.taskId || "", r.name, r.kind, r.status, String(r.durationMs), r.detail || ""]
                      .map(esc)
                      .join(",")
                  ),
                ];
                const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-scheduler-runs-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
            </div>
          </div>
          {runStats ? (
            <table>
              <thead>
                <tr>
                  <th>Task</th>
                  <th>Kind</th>
                  <th>Count</th>
                  <th>OK</th>
                  <th>Errors</th>
                  <th>Avg</th>
                  <th>Last</th>
                </tr>
              </thead>
              <tbody>
                {(() => {
                  const by = new Map<string, { kind: string; count: number; ok: number; sum: number; last: string }>();
                  for (const r of (runTask ? runs.filter((r) => r.name === runTask) : runs)) {
                    const g = by.get(r.name) ?? { kind: r.kind, count: 0, ok: 0, sum: 0, last: "" };
                    g.count++;
                    if (r.status === "ok") g.ok++;
                    g.sum += r.durationMs;
                    if (r.time > g.last) g.last = r.time;
                    by.set(r.name, g);
                  }
                  return [...by.entries()]
                    .sort((a, b) => (a[1].last < b[1].last ? 1 : -1))
                    .map(([name, g]) => (
                      <tr key={name}>
                        <td>{name}</td>
                        <td className="muted">{g.kind}</td>
                        <td>{g.count}</td>
                        <td>{g.ok}</td>
                        <td>{g.count - g.ok}</td>
                        <td className="muted" style={{ fontSize: 12 }}>
                          {Math.round(g.sum / g.count) < 1000
                            ? `${Math.round(g.sum / g.count)} ms`
                            : `${(g.sum / g.count / 1000).toFixed(1)} s`}
                        </td>
                        <td className="muted" style={{ fontSize: 12 }}>{new Date(g.last).toLocaleString()}</td>
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
                <th>Task</th>
                <th>Kind</th>
                <th>Status</th>
                <th>Duration</th>
                <th>Detail</th>
              </tr>
            </thead>
            <tbody>
              {(runTask ? runs.filter((r) => r.name === runTask) : runs).slice(0, 10).map((r, i) => (
                <tr key={i}>
                  <td className="muted" style={{ fontSize: 12 }}>{new Date(r.time).toLocaleString()}</td>
                  <td>{r.name}</td>
                  <td className="muted">{r.kind}</td>
                  <td>
                    <span className={`badge ${r.status === "ok" ? "ok" : "err"}`}>{r.status}</span>
                  </td>
                  <td className="muted" style={{ fontSize: 12 }}>
                    {r.durationMs < 1000 ? `${r.durationMs} ms` : `${(r.durationMs / 1000).toFixed(1)} s`}
                  </td>
                  <td className="muted" style={{ fontSize: 12, maxWidth: 360, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={r.detail}>
                    {r.detail || "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          )}
        </div>
      )}
    </div>
  );
}
