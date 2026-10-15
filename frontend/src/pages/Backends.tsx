import { useState } from "react";
import { api } from "../api/client";
import { useStore } from "../state/store";
import { ok, err } from "../ui/toast";
import { ConfirmDialog } from "../ui/confirm";

export default function Backends() {
  const { backends, refreshOne } = useStore();
  const [showPlugin, setShowPlugin] = useState(false);
  const [pluginJson, setPluginJson] = useState(
    '{\n  "id": "my-backend",\n  "name": "My Backend",\n  "description": "Custom backend",\n  "transport": "http",\n  "kind": "process",\n  "binary": {\n    "url": "https://example.com/my-backend-{platform}",\n    "sha256": ""\n  },\n  "command": ["my-backend", "--port", "{port}"]\n}'
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
