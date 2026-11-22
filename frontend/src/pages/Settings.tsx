import { useState, useEffect } from "react";
import { api, type Settings, type WebhookDelivery } from "../api/client";
import { ok, err } from "../ui/toast";

export default function SettingsPage() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [idleMin, setIdleMin] = useState(0);
  const [startupSec, setStartupSec] = useState(0);
  const [maxBodyMB, setMaxBodyMB] = useState(0);
  const [webhook, setWebhook] = useState("");
  const [whEvents, setWhEvents] = useState("");
  const [whSecret, setWhSecret] = useState("");
  const [whLog, setWhLog] = useState<WebhookDelivery[]>([]);
  const loadWhLog = () => api.webhookLog().then(setWhLog).catch(() => setWhLog([]));
  const [busy, setBusy] = useState(false);
  const [restoreFile, setRestoreFile] = useState<File | null>(null);
  const [restoreBusy, setRestoreBusy] = useState(false);
  const [backups, setBackups] = useState<{ name: string; size: number; created: string }[]>([]);
  const [snapBusy, setSnapBusy] = useState(false);

  useEffect(() => {
    api.backups().then(setBackups).catch(() => setBackups([]));
    loadWhLog();
  }, []);

  useEffect(() => {
    api.settings().then((s) => {
      setSettings(s);
      setIdleMin(s.idleUnloadMin || 0);
      setStartupSec(s.startupTimeoutSec || 0);
      setMaxBodyMB(s.maxBodyMB || 0);
      setWebhook(s.notifyWebhook || "");
      setWhEvents((s.notifyEvents || []).join(", "));
      setWhSecret(s.notifyWebhookSecret || "");
    }).catch(() => {});
  }, []);

  const toggle = async (key: "requireAuth" | "allowCORS" | "startOnDemand") => {
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

  const saveStartup = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api.updateSettings({ startupTimeoutSec: startupSec });
      setSettings(r);
      ok((r.startupTimeoutSec ?? 0) > 0 ? `instance startup timeout: ${r.startupTimeoutSec}s` : "startup timeout back to default (240s)");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const saveMaxBody = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api.updateSettings({ maxBodyMB });
      setSettings(r);
      ok((r.maxBodyMB ?? 0) > 0 ? `max request body: ${r.maxBodyMB} MiB` : "max request body back to default (64 MiB)");
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

  const saveWhEvents = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const list = whEvents
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean);
      const r = await api.updateSettings({ notifyEvents: list });
      setSettings(r);
      ok(r.notifyEvents?.length ? `webhook filter: ${r.notifyEvents.join(", ")}` : "webhook forwards all events");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const saveWhSecret = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api.updateSettings({ notifyWebhookSecret: whSecret.trim() });
      setSettings(r);
      ok(r.notifyWebhookSecret ? "webhook signing secret saved" : "webhook signing disabled");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const createSnapshot = async () => {
    setSnapBusy(true);
    try {
      const b = await api.createBackup();
      ok(`snapshot ${b.name} created`);
      api.backups().then(setBackups).catch(() => {});
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setSnapBusy(false);
    }
  };

  const testWebhook = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api.testNotifyWebhook();
      if (r.ok) ok("test notification delivered");
      else err(`webhook failed: ${r.error}`);
      loadWhLog();
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
              <h3 style={{ margin: 0, fontSize: 14 }}>Startup timeout</h3>
              <div className="spacer" />
              <input
                type="number"
                min={0}
                max={86400}
                style={{ width: 90 }}
                value={startupSec}
                onChange={(e) => setStartupSec(Math.max(0, Number(e.target.value) || 0))}
              />
              <span className="muted small">seconds</span>
              <button className="btn primary" disabled={busy} onClick={saveStartup}>
                Save
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              How long an instance start waits for the backend to become
              healthy. Raise for very large models on slow storage.
              0 uses the default (240 s).
            </p>
          </div>
          <div>
            <div className="row" style={{ marginBottom: 8, alignItems: "center" }}>
              <h3 style={{ margin: 0, fontSize: 14 }}>Max request body</h3>
              <div className="spacer" />
              <input
                type="number"
                min={0}
                max={4096}
                style={{ width: 90 }}
                value={maxBodyMB}
                onChange={(e) => setMaxBodyMB(Math.max(0, Math.min(4096, Number(e.target.value) || 0)))}
              />
              <span className="muted small">MiB</span>
              <button className="btn primary" disabled={busy} onClick={saveMaxBody}>
                Save
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              Gateway requests with a larger body are rejected with 413.
              0 uses the default (64 MiB).
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
              <button className="btn" disabled={busy || !webhook.trim()} onClick={testWebhook}>
                Test
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              Receives a JSON POST when an instance crashes or is idle-unloaded.
              Empty disables notifications.
            </p>
            <div className="row" style={{ marginTop: 8, alignItems: "center" }}>
              <span className="muted small" style={{ width: 90 }}>events</span>
              <input
                style={{ width: 320 }}
                placeholder="instance.crashed, instance.idle-unloaded (empty = all)"
                value={whEvents}
                onChange={(e) => setWhEvents(e.target.value)}
              />
              <button className="btn" disabled={busy} onClick={saveWhEvents}>
                Save
              </button>
            </div>
            <div className="row" style={{ marginTop: 8, alignItems: "center" }}>
              <span className="muted small" style={{ width: 90 }}>secret</span>
              <input
                type="password"
                style={{ width: 320 }}
                placeholder="HMAC signing key (optional)"
                value={whSecret}
                onChange={(e) => setWhSecret(e.target.value)}
              />
              <button className="btn" disabled={busy} onClick={saveWhSecret}>
                Save
              </button>
            </div>
            <p className="muted small" style={{ margin: "6px 0 0" }}>
              When set, every webhook POST carries X-LLMCtrl-Signature:
              sha256=HMAC(secret, body) so receivers can verify it.
            </p>
            {whLog.length > 0 && (
              <div style={{ marginTop: 10 }}>
                <span className="muted small">recent deliveries</span>
                <table style={{ marginTop: 4 }}>
                  <thead>
                    <tr>
                      <th>Time</th>
                      <th>Event</th>
                      <th>Result</th>
                    </tr>
                  </thead>
                  <tbody>
                    {whLog.slice(0, 5).map((d, i) => (
                      <tr key={i}>
                        <td className="muted" style={{ fontSize: 12 }}>{new Date(d.time).toLocaleTimeString()}</td>
                        <td className="mono" style={{ fontSize: 12 }}>{d.event || "—"}</td>
                        <td>
                          <span className={`badge ${d.ok ? "ok" : "err"}`}>{d.status || (d.ok ? 200 : "err")}</span>
                          {!d.ok && d.error ? <span className="muted" style={{ fontSize: 11 }}> {d.error}</span> : null}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
          <div>
            <div className="row" style={{ marginBottom: 8, alignItems: "center" }}>
              <h3 style={{ margin: 0, fontSize: 14 }}>CORS (gateway)</h3>
              <div className="spacer" />
              <button
                className={`btn ${settings?.allowCORS ? "primary" : ""}`}
                disabled={busy}
                onClick={() => toggle("allowCORS")}
              >
                {settings?.allowCORS ? "ON" : "OFF"}
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              When ON, the gateway answers cross-origin preflights and sends
              Access-Control-Allow-* headers so browser apps can call /v1/*
              directly.
            </p>
          </div>
          <div>
            <div className="row" style={{ marginBottom: 8, alignItems: "center" }}>
              <h3 style={{ margin: 0, fontSize: 14 }}>Start on demand</h3>
              <div className="spacer" />
              <button
                className={`btn ${settings?.startOnDemand ? "primary" : ""}`}
                disabled={busy}
                onClick={() => toggle("startOnDemand")}
              >
                {settings?.startOnDemand ? "ON" : "OFF"}
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              When ON, a gateway request for a model with an auto-start
              declaration cold-starts it; the request waits for the backend to
              become healthy.
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
        <div className="row" style={{ marginBottom: 10 }}>
          <button className="btn primary" disabled={snapBusy} onClick={createSnapshot}>
            {snapBusy ? "Creating…" : "Create snapshot now"}
          </button>
          <span className="muted small">Keeps the newest 7 snapshots.</span>
        </div>
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
