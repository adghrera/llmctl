import { useState, useEffect } from "react";
import { api, type Settings } from "../api/client";
import { ok, err } from "../ui/toast";

export default function SettingsPage() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.settings().then(setSettings).catch(() => {});
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
        </div>
      </div>
    </>
  );
}
