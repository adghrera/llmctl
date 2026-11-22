import { useEffect, useState } from "react";
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
  const [kb, setKb] = useState("");
  const [kbs, setKbs] = useState<KB[]>([]);
  const [webhookUrl, setWebhookUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [runs, setRuns] = useState<ScheduleRun[]>([]);

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
        kb: kind === "chat" && kb ? kb : undefined,
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
      setKb("");
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
              title="inject top knowledge-base chunks as context (F365)"
              value={kb}
              onChange={(e) => setKb(e.target.value)}
            >
              <option value="">no knowledge base</option>
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
        <h2>Scheduled Tasks ({schedules.length})</h2>
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
                  <td>{s.name}</td>
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
          <h2>Recent Runs</h2>
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
              {runs.slice(0, 10).map((r, i) => (
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
        </div>
      )}
    </div>
  );
}
