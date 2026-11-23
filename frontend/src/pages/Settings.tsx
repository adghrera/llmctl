import { useState, useEffect } from "react";
import { api, type Settings, type WebhookDelivery } from "../api/client";
import { ago } from "../lib/time";
import { ok, err } from "../ui/toast";

export default function SettingsPage() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [idleMin, setIdleMin] = useState(0);
  const [startupSec, setStartupSec] = useState(0);
  const [maxBodyMB, setMaxBodyMB] = useState(0);
  const [rateRPS, setRateRPS] = useState(0);
  const [usageRetentionDays, setUsageRetentionDays] = useState(0);
  const [logLevel, setLogLevel] = useState("info");
  const [backupKeep, setBackupKeep] = useState(0);
  const [rateBurst, setRateBurst] = useState(0);
  const [webhook, setWebhook] = useState("");
  const [whEvents, setWhEvents] = useState("");
  const [whSecret, setWhSecret] = useState("");
  const [whLog, setWhLog] = useState<WebhookDelivery[]>([]);
  const [whEvent, setWhEvent] = useState("");
  const [testEvent, setTestEvent] = useState(""); // custom test-event name (F592)
  const [testMsg, setTestMsg] = useState(""); // custom test message text (F697)
  const loadWhLog = () => api.webhookLog().then(setWhLog).catch(() => setWhLog([]));
  const shownWh = whLog
    .filter((d) => !whEvent || d.event === whEvent)
    .filter((d) => !whFailed || !d.ok); // failed-only view (F709)
  // Re-send a failed delivery's stored payload (F679), twin of `webhooks retry`.
  const retryDelivery = async (id: string) => {
    try {
      const r = await api.retryWebhook(id);
      if (r.ok) ok("delivery re-sent");
      else err(r.error || "retry failed");
      await loadWhLog();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };
  const [busy, setBusy] = useState(false);
  // Gateway response-cache card: counters + clear (F841).
  const [cacheSt, setCacheSt] = useState<{ entries: number; hits: number; misses: number; stores: number; maxEntries: number; ttlSeconds: number } | null>(null);
  const loadCache = () => api.cacheStats().then(setCacheSt).catch(() => setCacheSt(null));
  useEffect(() => {
    loadCache();
  }, []);
  const clearCache = async () => {
    try {
      const r = await api.cacheClear();
      ok(`cleared ${r.cleared} cached response(s)`);
      await loadCache();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };
  const [restoreFile, setRestoreFile] = useState<File | null>(null);
  const [restoreBusy, setRestoreBusy] = useState(false);
  const [backups, setBackups] = useState<{ name: string; size: number; created: string }[]>([]);
  const [snapLabel, setSnapLabel] = useState("");
  const [snapBusy, setSnapBusy] = useState(false);
  const [autoSnap, setAutoSnap] = useState(false); // live-refresh the snapshot list (CLI `backups -watch`, F610)
  const [autoWh, setAutoWh] = useState(false); // live-refresh the webhook log (CLI `webhooks -watch`, F615)
  const [whFailed, setWhFailed] = useState(false); // failed-only view, CLI `webhooks -failed` twin (F709)
  const [whStats, setWhStats] = useState(false); // per-event roll-up, CLI `webhooks -stats` twin (F749)
  // API console state (F589, UI counterpart of `llmctl api`).
  const [apiMethod, setApiMethod] = useState("GET");
  const [apiPath, setApiPath] = useState("/api/v1/status");
  const [apiBody, setApiBody] = useState("");
  const [apiResult, setApiResult] = useState("");
  const [apiBusy, setApiBusy] = useState(false);
  const [health, setHealth] = useState<Awaited<ReturnType<typeof api.doctor>> | null>(null);

  useEffect(() => {
    api.backups().then(setBackups).catch(() => setBackups([]));
    api.doctor().then(setHealth).catch(() => setHealth(null));
    loadWhLog();
  }, []);

  // Poll the snapshot list while the toggle is on (F610), so scheduled
  // backups appear without a reload.
  useEffect(() => {
    if (!autoSnap) return;
    const t = setInterval(() => api.backups().then(setBackups).catch(() => {}), 5000);
    return () => clearInterval(t);
  }, [autoSnap]);

  // Poll the webhook delivery log while the toggle is on (F615), so new
  // deliveries appear while testing a receiver.
  useEffect(() => {
    if (!autoWh) return;
    const t = setInterval(loadWhLog, 5000);
    return () => clearInterval(t);
  }, [autoWh]);

  useEffect(() => {
    api.settings().then((s) => {
      setSettings(s);
      setIdleMin(s.idleUnloadMin || 0);
      setStartupSec(s.startupTimeoutSec || 0);
      setMaxBodyMB(s.maxBodyMB || 0);
      setRateRPS(s.rateRPS || 0);
      setUsageRetentionDays(s.usageRetentionDays ?? 0);
      setLogLevel(s.logLevel || "info");
      setBackupKeep(s.backupKeep ?? 0);
      setRateBurst(s.rateBurst || 0);
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

  const saveRate = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api.updateSettings({ rateRPS, rateBurst });
      setSettings(r);
      ok((r.rateRPS ?? 0) > 0 ? `default rate: ${r.rateRPS} rps / burst ${r.rateBurst}` : "default rate back to CLI defaults");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const saveRetention = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api.updateSettings({ usageRetentionDays });
      setSettings(r);
      ok(
        (r.usageRetentionDays ?? 0) === -1
          ? "usage history kept forever"
          : (r.usageRetentionDays ?? 0) > 0
            ? `usage history kept ${r.usageRetentionDays} days`
            : "usage history retention back to default (90 days)",
      );
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const saveLogLevel = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api.updateSettings({ logLevel });
      setSettings(r);
      ok(`daemon log level: ${r.logLevel || "info"}`);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const saveBackupKeep = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const r = await api.updateSettings({ backupKeep });
      setSettings(r);
      ok(`snapshots kept: ${r.backupKeep || 7}`);
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
      const b = await api.createBackup(snapLabel.trim() || undefined);
      ok(`snapshot ${b.name} created`);
      setSnapLabel("");
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
      const r = await api.testNotifyWebhook(
        testEvent.trim() || undefined,
        testMsg.trim() || undefined
      );
      if (r.ok) ok(testEvent.trim() ? `test "${testEvent.trim()}" delivered` : "test notification delivered");
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
      {health && (
        <div className="card">
          <h2>System health</h2>
          <table>
            <thead>
              <tr>
                <th>Check</th>
                <th>Detail</th>
              </tr>
            </thead>
            <tbody>
              {health.checks.map((c) => (
                <tr key={c.name}>
                  <td>
                    <span className={`badge ${c.status === "ok" ? "ok" : c.status === "error" ? "err" : "warn"}`}>{c.status}</span>{" "}
                    <span className="mono" style={{ fontSize: 12 }}>{c.name}</span>
                  </td>
                  <td className="muted" style={{ fontSize: 12 }}>{c.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="row" style={{ marginTop: 10 }}>
            <button className="btn sm" onClick={() => api.doctor().then(setHealth).catch(() => {})}>
              Re-run checks
            </button>
            <button
              className="btn sm"
              title="download the checks as CSV — same columns as llmctl doctor --csv (F724)"
              onClick={() => {
                const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                const rows = [
                  "name,status,detail",
                  ...health.checks.map((c) => [c.name, c.status, c.detail || ""].map(esc).join(",")),
                ];
                const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-doctor-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
            <button
              className="btn sm"
              title="download the checks as JSON (CLI: llmctl doctor -json)"
              onClick={() => {
                const blob = new Blob([JSON.stringify(health, null, 2)], { type: "application/json" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-doctor-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export JSON
            </button>
            <button
              className="btn sm danger"
              title="stop the daemon (CLI: llmctl down) — running instances are left as they are (F771)"
              onClick={async () => {
                if (!confirm("Shut down the daemon? The web UI will stop responding.")) return;
                try {
                  await api.shutdown();
                  ok("daemon shutting down — reload once it is back up");
                } catch (e) {
                  err(e instanceof Error ? e.message : String(e));
                }
              }}
            >
              Shutdown daemon
            </button>
            <span className="muted" style={{ fontSize: 12 }}>
              also available as <span className="mono">llmctl doctor</span>
            </span>
          </div>
        </div>
      )}
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
              <h3 style={{ margin: 0, fontSize: 14 }}>Default rate limit</h3>
              <div className="spacer" />
              <input
                type="number"
                min={0}
                step="0.1"
                style={{ width: 80 }}
                value={rateRPS}
                onChange={(e) => setRateRPS(Math.max(0, Number(e.target.value) || 0))}
              />
              <span className="muted small">rps</span>
              <input
                type="number"
                min={0}
                style={{ width: 80 }}
                value={rateBurst}
                onChange={(e) => setRateBurst(Math.max(0, Number(e.target.value) || 0))}
              />
              <span className="muted small">burst</span>
              <button className="btn primary" disabled={busy} onClick={saveRate}>
                Save
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              Fallback per-key rate limit for keys without their own limit.
              0 uses the CLI defaults (20 rps, burst 60).
            </p>
          </div>
          <div>
            <div className="row" style={{ marginBottom: 8, alignItems: "center" }}>
              <h3 style={{ margin: 0, fontSize: 14 }}>Usage retention</h3>
              <div className="spacer" />
              <input
                type="number"
                min={-1}
                style={{ width: 90 }}
                value={usageRetentionDays}
                onChange={(e) => setUsageRetentionDays(Math.max(-1, Number(e.target.value) || 0))}
              />
              <span className="muted small">days</span>
              <button className="btn primary" disabled={busy} onClick={saveRetention}>
                Save
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              How many UTC days of daily usage history are kept; older days are
              pruned. 0 = default (90), -1 = keep forever.
            </p>
          </div>
          <div>
            <div className="row" style={{ marginBottom: 8, alignItems: "center" }}>
              <h3 style={{ margin: 0, fontSize: 14 }}>Daemon log level</h3>
              <div className="spacer" />
              <select className="oc-input" style={{ width: 110 }} value={logLevel} onChange={(e) => setLogLevel(e.target.value)}>
                <option value="info">info</option>
                <option value="debug">debug</option>
                <option value="warn">warn</option>
                <option value="error">error</option>
              </select>
              <button className="btn primary" disabled={busy} onClick={saveLogLevel}>
                Save
              </button>
            </div>
            <p className="muted small" style={{ margin: 0 }}>
              Verbosity of the daemon log (~/.llmctl/daemon.log). Takes effect
              immediately and persists across restarts.
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
              <input
                className="oc-input"
                style={{ width: 160 }}
                placeholder="test (event name)"
                title="event name for the test payload (CLI: llmctl webhooks test -event E)"
                value={testEvent}
                onChange={(e) => setTestEvent(e.target.value)}
              />
              <input
                className="oc-input"
                style={{ width: 180 }}
                placeholder="message text (optional)"
                title="message text for the test payload (CLI: llmctl webhooks test -message M, F697)"
                value={testMsg}
                onChange={(e) => setTestMsg(e.target.value)}
              />
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
                <div className="row" style={{ alignItems: "center", gap: 8 }}>
                  <span className="muted small">recent deliveries</span>
                  <div className="spacer" />
                  <label
                    style={{ display: "inline-flex", gap: 6, alignItems: "center", fontSize: 12 }}
                    title="poll every 5s so new deliveries appear while testing a receiver (CLI: llmctl webhooks -watch N, F615)"
                  >
                    <input type="checkbox" checked={autoWh} onChange={(e) => setAutoWh(e.target.checked)} />
                    Auto-refresh
                  </label>
                  <label
                    style={{ display: "inline-flex", gap: 6, alignItems: "center", fontSize: 12 }}
                    title="show only failed deliveries (CLI: llmctl webhooks -failed, F709)"
                  >
                    <input type="checkbox" checked={whFailed} onChange={(e) => setWhFailed(e.target.checked)} />
                    Failed only
                  </label>
                  <label className="muted small" style={{ display: "flex", alignItems: "center", gap: 6 }}>
                    event
                    <select
                      style={{ width: 160 }}
                      value={whEvent}
                      onChange={(e) => setWhEvent(e.target.value)}
                    >
                      <option value="">all</option>
                      {[...new Set(whLog.map((d) => d.event).filter(Boolean))].map((ev) => (
                        <option key={ev} value={ev}>{ev}</option>
                      ))}
                    </select>
                  </label>
                  <button
                    className="btn sm"
                    onClick={() => {
                      // Same columns as `llmctl webhooks --csv` (F416); filter-aware.
                      const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                      const rows = [
                        "id,time,event,url,status,ok,error",
                        ...shownWh.map((d) =>
                          [d.id || "", d.time, d.event || "", d.url, String(d.status ?? ""), String(d.ok), d.error || ""]
                            .map(esc)
                            .join(",")
                        ),
                      ];
                      const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                      const a = document.createElement("a");
                      a.href = URL.createObjectURL(blob);
                      a.download = `llmctl-webhooks-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
                      a.click();
                      URL.revokeObjectURL(a.href);
                    }}
                  >
                    Export CSV
                  </button>
                  <button
                    className="btn sm"
                    title="download the delivery log as JSON (CLI: llmctl webhooks -json)"
                    onClick={() => {
                      const blob = new Blob([JSON.stringify(shownWh, null, 2)], { type: "application/json" });
                      const a = document.createElement("a");
                      a.href = URL.createObjectURL(blob);
                      a.download = `llmctl-webhooks-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
                      a.click();
                      URL.revokeObjectURL(a.href);
                    }}
                  >
                    Export JSON
                  </button>
                  <button
                    className="btn sm"
                    aria-pressed={whStats}
                    disabled={shownWh.length === 0}
                    title="group the delivery log per event — CLI: llmctl webhooks -stats (F749)"
                    onClick={() => setWhStats(!whStats)}
                  >
                    {whStats ? "Show list" : "Stats"}
                  </button>
                </div>
                {whStats ? (
                  <table style={{ marginTop: 4 }}>
                    <thead>
                      <tr>
                        <th>Event</th>
                        <th>Total</th>
                        <th>Ok</th>
                        <th>Failed</th>
                        <th>Last</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(() => {
                        // Per-event roll-up, same columns as `llmctl webhooks -stats` (F749).
                        const m = new Map<string, { total: number; ok: number; failed: number; last: string }>();
                        for (const d of shownWh) {
                          const ev = d.event || "(none)";
                          const r = m.get(ev) ?? { total: 0, ok: 0, failed: 0, last: "" };
                          r.total++;
                          if (d.ok) r.ok++; else r.failed++;
                          if (d.time > r.last) r.last = d.time;
                          m.set(ev, r);
                        }
                        return [...m.entries()]
                          .sort((a, b) => b[1].total - a[1].total)
                          .map(([ev, r]) => (
                            <tr key={ev}>
                              <td className="mono" style={{ fontSize: 12 }}>{ev}</td>
                              <td className="oc-num">{r.total}</td>
                              <td className="oc-num">{r.ok}</td>
                              <td className="oc-num">{r.failed}</td>
                              <td className="muted" style={{ fontSize: 12 }} title={new Date(r.last).toLocaleString()}>{ago(r.last)}</td>
                            </tr>
                          ));
                      })()}
                    </tbody>
                  </table>
                ) : (
                <table style={{ marginTop: 4 }}>
                  <thead>
                    <tr>
                      <th>Time</th>
                      <th>Event</th>
                      <th>Result</th>
                    </tr>
                  </thead>
                  <tbody>
                    {shownWh
                      .slice(0, 20)
                      .map((d, i) => (
                      <tr key={i}>
                        <td className="muted" style={{ fontSize: 12 }} title={new Date(d.time).toLocaleString()}>{ago(d.time)}</td>
                        <td className="mono" style={{ fontSize: 12 }}>{d.event || "—"}</td>
                        <td>
                          <span className={`badge ${d.ok ? "ok" : "err"}`}>{d.status || (d.ok ? 200 : "err")}</span>
                          {!d.ok && d.error ? <span className="muted" style={{ fontSize: 11 }}> {d.error}</span> : null}
                          {!d.ok && d.id ? (
                            <button className="btn sm" style={{ marginLeft: 6 }} title="re-send this delivery (CLI: llmctl webhooks retry)" onClick={() => retryDelivery(d.id!)}>
                              Retry
                            </button>
                          ) : null}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                )}
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
        <h2>Response cache</h2>
        <p className="muted small" style={{ margin: "4px 0 12px" }}>
          Opt-in gateway cache: clients send <code>X-LLMCtrl-Cache: 1</code> on a
          non-streaming chat call and identical requests within the TTL are
          answered from memory without an upstream. Cache hits log zero tokens.
          CLI: <code>llmctl cache stats|clear</code>.
        </p>
        <div className="row" style={{ gap: 8, alignItems: "center", flexWrap: "wrap" }}>
          {cacheSt ? (
            <span style={{ fontSize: 13 }}>
              {cacheSt.entries}/{cacheSt.maxEntries} entries · {cacheSt.hits} hits · {cacheSt.misses} misses ·{" "}
              {cacheSt.stores} stored · TTL {cacheSt.ttlSeconds}s
            </span>
          ) : (
            <span className="muted" style={{ fontSize: 13 }}>gateway not reachable</span>
          )}
          <button className="btn sm" onClick={loadCache} title="refresh the cache counters">
            Refresh
          </button>
          <button className="btn sm danger" onClick={clearCache} title="drop every cached response (CLI: llmctl cache clear)">
            Clear cache
          </button>
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
          <label
            style={{ display: "inline-flex", gap: 6, alignItems: "center", fontSize: 12 }}
            title="poll every 5s so scheduled backups appear without a reload (CLI: llmctl backups -watch N, F610)"
          >
            <input type="checkbox" checked={autoSnap} onChange={(e) => setAutoSnap(e.target.checked)} />
            Auto-refresh
          </label>
          <input
            className="oc-input"
            style={{ width: 160 }}
            placeholder="label (optional)"
            title="embedded in the snapshot file name (CLI: llmctl backups create -name LABEL)"
            value={snapLabel}
            onChange={(e) => setSnapLabel(e.target.value)}
          />
          <button className="btn primary" disabled={snapBusy} onClick={createSnapshot}>
            {snapBusy ? "Creating…" : "Create snapshot now"}
          </button>
          <button
            className="btn"
            disabled={backups.length === 0}
            onClick={() => {
              // Same columns as `llmctl backups list --csv` (F398).
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "name,size_bytes,created",
                ...backups.map((b) => [b.name, String(b.size), b.created].map(esc).join(",")),
              ];
              const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-backups-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export CSV
          </button>
          <button
            className="btn"
            disabled={backups.length === 0}
            title="download the snapshot list as JSON (CLI: llmctl backups list -json)"
            onClick={() => {
              const blob = new Blob([JSON.stringify(backups, null, 2)], { type: "application/json" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-backups-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export JSON
          </button>
          <button
            className="btn"
            disabled={snapBusy || backups.length === 0}
            title="delete all but the newest snapshots (CLI: llmctl backups prune)"
            onClick={async () => {
              const keep = backupKeep || 7;
              if (backups.length <= keep) {
                ok(`nothing to prune (keeping newest ${keep})`);
                return;
              }
              if (!confirm(`Prune ${backups.length - keep} old snapshot(s), keeping the newest ${keep}?`)) return;
              try {
                const r = await api.pruneBackups(keep);
                ok(`pruned ${r.deleted.length} snapshot(s), keeping newest ${r.keep}`);
                api.backups().then(setBackups).catch(() => {});
              } catch (e) {
                err(e instanceof Error ? e.message : String(e));
              }
            }}
          >
            Prune old
          </button>
          <button
            className="btn"
            disabled={snapBusy || backups.length === 0}
            title="check every snapshot's integrity without restoring (CLI: llmctl backups verify -all)"
            onClick={async () => {
              setSnapBusy(true);
              try {
                const results = await Promise.all(backups.map((b) => api.verifyBackup(b.name).catch((e) => ({ ok: false, error: e instanceof Error ? e.message : String(e) }))));
                const bad = results.filter((r) => !r.ok);
                if (bad.length === 0) ok(`all ${backups.length} snapshot(s) ok — state.json valid`);
                else err(`${bad.length}/${backups.length} snapshot(s) failed: ${bad.map((b) => (b as { error?: string }).error).join("; ")}`);
              } finally {
                setSnapBusy(false);
              }
            }}
          >
            Verify all
          </button>
          <span className="muted small">Keeps the newest {backupKeep || 7} snapshots.</span>
        </div>
        <div className="row" style={{ marginBottom: 10, alignItems: "center" }}>
          <label className="muted small" style={{ display: "flex", alignItems: "center", gap: 6 }}>
            Snapshots kept
            <input
              className="oc-input"
              type="number"
              min={0}
              max={1000}
              style={{ width: 80 }}
              value={backupKeep}
              onChange={(e) => setBackupKeep(parseInt(e.target.value || "0", 10))}
            />
          </label>
          <button className="btn" disabled={busy} onClick={saveBackupKeep}>
            Save
          </button>
          <span className="muted small">0 = default (7). Applies to new snapshots.</span>
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
                    <button
                      className="btn sm"
                      title="check archive integrity without restoring"
                      onClick={async () => {
                        try {
                          const r = await api.verifyBackup(b.name);
                          if (r.ok) ok(`${b.name}: ok — ${r.entries} entries, state.json valid`);
                          else err(`${b.name}: ${r.error}`);
                        } catch (e) {
                          err(e instanceof Error ? e.message : String(e));
                        }
                      }}
                    >
                      Verify
                    </button>{" "}
                    <button
                      className="btn sm"
                      title="import this snapshot into the state dir (restart the daemon to load it)"
                      onClick={async () => {
                        if (!confirm(`Restore ${b.name}? This overwrites the current state. Restart the daemon afterwards to load it.`)) return;
                        try {
                          const r = await api.restoreStoredBackup(b.name);
                          ok(`restored ${r.files} file(s) — restart the daemon to load the state`);
                        } catch (e) {
                          err(e instanceof Error ? e.message : String(e));
                        }
                      }}
                    >
                      Restore
                    </button>{" "}
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
      <div className="card">
        <h2>API console</h2>
        <p className="muted small" style={{ margin: "4px 0 12px" }}>
          Send any request to the management API — the browser counterpart of
          <code> llmctl api &lt;METHOD&gt; /path [-d JSON]</code> (F589).
        </p>
        <div className="row" style={{ gap: 6, marginBottom: 8 }}>
          <select className="oc-input" style={{ width: 100 }} value={apiMethod} onChange={(e) => setApiMethod(e.target.value)}>
            {["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => (
              <option key={m}>{m}</option>
            ))}
          </select>
          <input
            className="oc-input"
            style={{ flex: 1 }}
            placeholder="/api/v1/status"
            value={apiPath}
            onChange={(e) => setApiPath(e.target.value)}
          />
          <button
            className="btn primary"
            disabled={apiBusy || !apiPath.startsWith("/")}
            onClick={async () => {
              setApiBusy(true);
              try {
                const opts: RequestInit = { method: apiMethod };
                if (apiBody && apiMethod !== "GET") {
                  opts.headers = { "Content-Type": "application/json" };
                  opts.body = apiBody;
                }
                const r = await fetch(apiPath, opts);
                const text = await r.text();
                let pretty = text;
                try {
                  pretty = JSON.stringify(JSON.parse(text), null, 2);
                } catch {
                  /* keep raw text */
                }
                setApiResult(`${r.status} ${r.statusText}\n\n${pretty}`);
              } catch (e) {
                setApiResult(`error: ${e instanceof Error ? e.message : String(e)}`);
              } finally {
                setApiBusy(false);
              }
            }}
          >
            {apiBusy ? "Sending…" : "Send"}
          </button>
          <button
            className="btn sm"
            disabled={!apiPath.startsWith("/")}
            title="copy a curl command for this request (F787)"
            onClick={() => {
              const parts = [`curl -X ${apiMethod} ${window.location.origin}${apiPath}`];
              if (apiBody && apiMethod !== "GET") {
                parts.push(`  -H "Content-Type: application/json"`);
                const q = `'${apiBody.replace(/'/g, `'\\''`)}'`;
                parts.push(`  -d ${q}`);
              }
              navigator.clipboard
                ?.writeText(parts.join(" \\\n") + "\n")
                .then(() => ok("copied curl command"))
                .catch(() => err("clipboard unavailable"));
            }}
          >
            Copy as cURL
          </button>
        </div>
        {(apiMethod === "POST" || apiMethod === "PUT" || apiMethod === "PATCH") && (
          <textarea
            className="oc-input"
            style={{ width: "100%", minHeight: 60, fontFamily: "var(--font-mono)", fontSize: 12 }}
            placeholder='request body (JSON), e.g. {"name":"demo"}'
            value={apiBody}
            onChange={(e) => setApiBody(e.target.value)}
          />
        )}
        {apiResult && (
          <>
            <button
              className="btn sm"
              style={{ marginTop: 10 }}
              title="copy the API response to the clipboard (F772)"
              onClick={() => {
                navigator.clipboard
                  ?.writeText(apiResult)
                  .then(() => ok("copied"))
                  .catch(() => {});
              }}
            >
              Copy result
            </button>
            <pre
              className="mono"
              style={{ marginTop: 10, maxHeight: 300, overflow: "auto", fontSize: 12, whiteSpace: "pre-wrap" }}
            >
              {apiResult}
            </pre>
          </>
        )}
      </div>
    </>
  );
}
