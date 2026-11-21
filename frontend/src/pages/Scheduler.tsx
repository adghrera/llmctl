import { useEffect, useState } from "react";
import { api, ScheduleSpec } from "../api/client";
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
  const [webhookUrl, setWebhookUrl] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () =>
    api
      .schedules()
      .then(setSchedules)
      .catch((e) => err(e instanceof Error ? e.message : String(e)));

  useEffect(() => {
    load();
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
                      ? new Date(s.next_run).toLocaleString()
                      : "—"}
                  </td>
                  <td style={{ fontSize: 12, opacity: 0.7 }}>
                    {s.last_run
                      ? new Date(s.last_run).toLocaleString()
                      : "—"}
                  </td>
                  <td style={{ textAlign: "right" }}>
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
    </div>
  );
}
