import { useState } from "react";
import { api } from "../api/client";
import { useStore } from "../state/store";
import { ok, err } from "../ui/toast";
import { ConfirmDialog } from "../ui/confirm";

export default function Backends() {
  const { backends, refreshOne } = useStore();
  const [showPlugin, setShowPlugin] = useState(false);
  const [pluginJson, setPluginJson] = useState(
    '{\n  "id": "my-backend",\n  "name": "My Backend",\n  "description": "Custom backend",\n  "transport": "http",\n  "kind": "process",\n  "binary": "my-backend",\n  "args": ["--port", "{port}"],\n  "install": { "kind": "url", "url": "https://example.com/my-backend-{os}-{arch}{ext}" }\n}'
  );
  const [busy, setBusy] = useState<string | null>(null);
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const [confirmMessage, setConfirmMessage] = useState<string>("");

  const act = async (id: string, fn: () => Promise<unknown>, msg: string) => {
    setBusy(id);
    try {
      await fn();
      ok(msg);
      await refreshOne("backends");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  const addPlugin = async () => {
    setBusy("plugin");
    try {
      const parsed = JSON.parse(pluginJson);
      await api.addPlugin(parsed);
      ok(`added plugin backend ${parsed.id}`);
      setShowPlugin(false);
      await refreshOne("backends");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  // Remove a plugin backend registration (CLI: llmctl backends remove, F658).
  const removePlugin = (id: string) => {
    if (!window.confirm(`Remove plugin backend "${id}"? Its registration is deleted.`)) return;
    act(id, () => api.removePlugin(id), `removed plugin ${id}`);
  };

  const handleUninstall = (id: string) => {
    setConfirmId(id);
    setConfirmMessage(`Uninstall backend ${id}? This cannot be undone.`);
  };

  const handleConfirm = async () => {
    if (!confirmId) return;
    setConfirmId(null);
    await act(confirmId, () => api.uninstallBackend(confirmId), `uninstalled ${confirmId}`);
  };

  const handleCancel = () => {
    setConfirmId(null);
  };

  return (
    <>
      <div className="card">
        <div className="row">
          <h2>Backends</h2>
          <div className="spacer" />
          <button
            className="btn"
            disabled={backends.length === 0}
            onClick={() => {
              // Same columns as `llmctl backends list --csv` (F404).
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "id,transport,kind,installed,version,description",
                ...backends.map((b) =>
                  [b.id, b.transport, b.kind, String(b.installed), b.version || "", b.description]
                    .map(esc)
                    .join(",")
                ),
              ];
              const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-backends-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export CSV
          </button>
          <button
            className="btn sm"
            disabled={backends.length === 0}
            title="download the backends list as JSON (CLI: llmctl backends list -json)"
            onClick={() => {
              const blob = new Blob([JSON.stringify(backends, null, 2)], { type: "application/json" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-backends-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export JSON
          </button>
          <button className="btn" onClick={() => setShowPlugin((s) => !s)}>
            {showPlugin ? "Cancel" : "+ Add plugin backend"}
          </button>
        </div>
        {showPlugin && (
          <div style={{ marginTop: 12 }}>
            <label>Plugin backend JSON</label>
            <textarea
              rows={10}
              className="mono"
              value={pluginJson}
              onChange={(e) => setPluginJson(e.target.value)}
            />
            <div className="row" style={{ marginTop: 8 }}>
              <button className="btn primary" disabled={busy === "plugin"} onClick={addPlugin}>
                Add backend
              </button>
            </div>
          </div>
        )}
      </div>
      <div className="card">
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Transport</th>
              <th>Kind</th>
              <th>Status</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {backends.map((b) => (
              <tr key={b.id}>
                <td className="mono">{b.id}</td>
                <td>
                  {b.name}
                  {b.plugin && <span className="badge" style={{ marginLeft: 6 }}>plugin</span>}
                </td>
                <td>{b.transport}</td>
                <td>{b.kind}</td>
                <td>
                  {b.installed ? (
                    <>
                      <button
                        className="btn sm primary"
                        disabled={busy === b.id}
                        onClick={() => act(b.id, () => api.installBackend(b.id), `install started for ${b.id}`)}
                      >
                        Install
                      </button>
                      <button
                        className="btn sm danger"
                        disabled={busy === b.id}
                        onClick={() => handleUninstall(b.id)}
                      >
                        Uninstall
                      </button>
                    </>
                  ) : (
                    <button
                      className="btn sm primary"
                      disabled={busy === b.id}
                      onClick={() => act(b.id, () => api.installBackend(b.id), `install started for ${b.id}`)}
                    >
                      Install
                    </button>
                  )}
                </td>
                <td style={{ textAlign: "right", whiteSpace: "nowrap" }}>
                  {b.installed ? (
                    <button
                      className="btn sm danger"
                      disabled={busy === b.id}
                      onClick={() => act(b.id, () => api.uninstallBackend(b.id), `uninstalled ${b.id}`)}
                    >
                      Uninstall
                    </button>
                  ) : (
                    <button
                      className="btn sm primary"
                      disabled={busy === b.id}
                      onClick={() => act(b.id, () => api.installBackend(b.id), `install started for ${b.id}`)}
                    >
                      Install
                    </button>
                  )}
                  {b.plugin && (
                    <button
                      className="btn sm danger"
                      disabled={busy === b.id}
                      title="remove this plugin backend registration (CLI: llmctl backends remove)"
                      onClick={() => removePlugin(b.id)}
                    >
                      Remove
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <ConfirmDialog
        open={!!confirmId}
        onConfirm={handleConfirm}
        onCancel={handleCancel}
        title="Confirm uninstall"
        message={confirmMessage}
        confirmText="Uninstall"
        cancelText="Cancel"
      />
    </>
  );
}
