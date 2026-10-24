import { useEffect, useState } from "react";
import { api, ScheduleSpec } from "../api/client";
import { ok, err } from "../ui/toast";

export default function Scheduler() {
  const [schedules, setSchedules] = useState<ScheduleSpec[]>([]);
  const [name, setName] = useState("");
  const [cronExpr, setCronExpr] = useState("0 9 * * *");
  const [model, setModel] = useState("");
  const [prompt, setPrompt] = useState("");
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
    if (!name.trim() || !cronExpr.trim() || !model.trim() || !prompt.trim()) {
      err("all fields are required");
      return;
    }
    setBusy(true);
    try {
      await api.createSchedule({ name, cron_expr: cronExpr, model, prompt });
      ok("schedule created");
      setName("");
      setCronExpr("0 9 * * *");
      setModel("");
      setPrompt("");
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
          Schedule agent chat tasks to run at regular intervals using cron
          expressions. Each run sends a prompt to the model and saves the
          result as a new conversation.
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
          <input
            style={{ flex: 1, minWidth: 140 }}
            placeholder="Model id"
            value={model}
            onChange={(e) => setModel(e.target.value)}
          />
          <input
            style={{ flex: 2, minWidth: 200 }}
            placeholder="Prompt to send"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
          />
          <button
            className="btn primary"
            disabled={
              busy ||
              !name.trim() ||
              !cronExpr.trim() ||
              !model.trim() ||
              !prompt.trim()
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
                <th>Model</th>
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
                  <td style={{ fontFamily: "monospace", fontSize: 12 }}>
                    {s.model}
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
