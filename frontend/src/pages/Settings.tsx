import { useState, useEffect } from "react";
import { api, type Settings } from "../api/client";
import { ok, err } from "../ui/toast";

export default function SettingsPage() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [idleMin, setIdleMin] = useState(0);
  const [webhook, setWebhook] = useState("");
  const [busy, setBusy] = useState(false);
  const [restoreFile, setRestoreFile] = useState<File | null>(null);
  const [restoreBusy, setRestoreBusy] = useState(false);
  const [backups, setBackups] = useState<{ name: string; size: number; created: string }[]>([]);

  useEffect(() => {
    api.backups().then(setBackups).catch(() => setBackups([]));
  }, []);

  useEffect(() => {
    api.settings().then((s) => {
      setSettings(s);
      setIdleMin(s.idleUnloadMin || 0);
      setWebhook(s.notifyWebhook || "");
    }).catch(() => {});
  }, []);

  const toggle = async (key: "requireAuth") => {
    if (busy) return;
    setBusy(true);
    try {
      const val = !settings?.[key];
      const r = await api.updateSettings({ [key]: val });
      setSettings(r);
      ok(`${key} ${val ? "enabled" : "disabled"}`);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const saveIdle = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api.updateSettings({ idleUnloadMin: idleMin });
      setSettings(r);
      ok(r.idleUnloadMin > 0 ? `instances auto-stop after ${r.idleUnloadMin} min idle` : "idle auto-unload disabled");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const saveWebhook = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api.updateSettings({ notifyWebhook: webhook.trim() });
      setSettings(r);
      ok(r.notifyWebhook ? "notify webhook saved" : "notify webhook cleared");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const restore = async () => {
    if (!restoreFile) return;
    if (!window.confirm("Restore will overwrite current config, chats, knowledge bases, memory and more. Continue?")) return;
    setRestoreBusy(true);
    try {
      const r = await api.restoreBackup(restoreFile);
      ok(`restored ${r.files} file(s) — reload to see changes`);
      setRestoreFile(null);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setRestoreBusy(false);
    }
  };

  return (
    <>
      <div className="card">
        <h2>Gateway Settings</h2>
        <div style={{ display: "flex", flexDirection: "column", gap: 20 }}>
          <div>
            <div className="row" style={{ marginBottom: 8 }}>
              <h3 style={{ margin: 0, fontSize: 14 }}>Require API Key</h3>
              <div className="spacer" />
              <button
                className={`btn ${settings?.requireAuth ? "primary" : ""}`}
                disabled={busy}
                onClick={() => toggle("requireAuth")}
              >
                {settings?.requireAuth ? "ON" : "OFF"}
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              When ON, all /v1/* endpoints require Authorization: Bearer.
              When OFF, no key is needed (anyone can use the gateway).
            </p>
          </div>
          <div>
            <div className="row" style={{ marginBottom: 8, alignItems: "center" }}>
              <h3 style={{ margin: 0, fontSize: 14 }}>Idle auto-unload</h3>
              <div className="spacer" />
              <input
                type="number"
                min={0}
                max={1440}
                style={{ width: 90 }}
                value={idleMin}
                onChange={(e) => setIdleMin(Math.max(0, Number(e.target.value) || 0))}
              />
              <span className="muted small">minutes</span>
              <button className="btn primary" disabled={busy} onClick={saveIdle}>
                Save
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              Running instances that serve no gateway request for this many
              minutes are stopped automatically (frees VRAM). 0 disables it.
            </p>
          </div>
          <div>
            <div className="row" style={{ marginBottom: 8, alignItems: "center" }}>
              <h3 style={{ margin: 0, fontSize: 14 }}>Notify webhook</h3>
              <div className="spacer" />
              <input
                style={{ width: 320 }}
                placeholder="https://hooks.example.com/llmctl"
                value={webhook}
                onChange={(e) => setWebhook(e.target.value)}
              />
              <button className="btn primary" disabled={busy} onClick={saveWebhook}>
                Save
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              Receives a JSON POST when an instance crashes or is idle-unloaded.
              Empty disables notifications.
            </p>
          </div>
        </div>
      </div>

      <div className="card">
        <h2>Backup &amp; Restore</h2>
        <p className="muted small" style={{ margin: "4px 0 12px" }}>
          Backups bundle configuration, API keys, chats, knowledge bases,
          memory, skills, tools, scheduler jobs and plugins into one archive.
          Backend binaries, model files and logs are excluded (re-installable
          / re-downloadable).
        </p>
        <div className="row" style={{ gap: 8, marginBottom: 12 }}>
          <a className="btn primary" href="/api/v1/backup" download>
            Download backup (.tar.gz)
          </a>
        </div>
        <div className="row" style={{ gap: 8, alignItems: "center", flexWrap: "wrap" }}>
          <input
            type="file"
            accept=".tar.gz,.gz,application/gzip"
            onChange={(e) => setRestoreFile(e.target.files?.[0] ?? null)}
            disabled={restoreBusy}
          />
          <button className="btn danger" disabled={restoreBusy || !restoreFile} onClick={restore}>
            {restoreBusy ? "Restoring…" : "Restore from file"}
          </button>
          {restoreFile && (
            <span style={{ fontSize: 12, opacity: 0.7 }}>{restoreFile.name}</span>
          )}
        </div>
      </div>

      <div className="card">
        <h2>Scheduled backups</h2>
        <p className="muted small" style={{ margin: "4px 0 12px" }}>
          Snapshots created by scheduler tasks of kind "backup"
          (~/.llmctl/backups). Create one on the Scheduler page.
        </p>
        {backups.length ? (
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Size</th>
                <th>Created</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {backups.map((b) => (
                <tr key={b.name}>
                  <td className="mono">{b.name}</td>
                  <td>{(b.size / 1024).toFixed(1)} KB</td>
                  <td>{new Date(b.created).toLocaleString()}</td>
                  <td style={{ textAlign: "right" }}>
                    <a className="btn sm" href={`/api/v1/backups/${encodeURIComponent(b.name)}`} download>
                      Download
                    </a>{" "}
                    <button
                      className="btn sm danger"
                      onClick={async () => {
                        try {
                          await api.deleteBackup(b.name);
                          ok("backup deleted");
                          api.backups().then(setBackups).catch(() => {});
                        } catch (e) {
                          err(e instanceof Error ? e.message : String(e));
                        }
                      }}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <div className="empty">no scheduled backups yet</div>
        )}
      </div>
    </>
  );
}
