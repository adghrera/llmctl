import { useState } from "react";
import { api } from "../api/client";
import { useStore } from "../state/store";
import { ok, err } from "../ui/toast";

export default function Keys() {
  const { keys, refreshOne } = useStore();
  const [name, setName] = useState("");
  const [created, setCreated] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const create = async () => {
    setBusy(true);
    try {
      const r = await api.createKey(name);
      setCreated(r.key);
      setName("");
      ok(`created key ${r.prefix}…`);
      await refreshOne("keys");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const revoke = async (id: string) => {
    setBusy(true);
    try {
      await api.revokeKey(id);
      ok("revoked key");
      await refreshOne("keys");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="card">
        <h2>Create an API key</h2>
        <div className="row">
          <input
            style={{ flex: 1, minWidth: 200 }}
            placeholder="Key name (e.g. my-app)"
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && create()}
          />
          <button className="btn primary" disabled={busy || !name} onClick={create}>
            Create
          </button>
        </div>
        {created && (
          <div style={{ marginTop: 12 }}>
            <label>New key — copy it now, it is shown only once</label>
            <div className="keybox">{created}</div>
            <div className="row" style={{ marginTop: 8 }}>
              <button
                className="btn sm"
                onClick={() => navigator.clipboard.writeText(created)}
              >
                Copy
              </button>
              <button className="btn sm" onClick={() => setCreated(null)}>
                Dismiss
              </button>
            </div>
          </div>
        )}
      </div>
      <div className="card">
        <h2>API keys</h2>
        {keys.length ? (
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Prefix</th>
                <th>Created</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id}>
                  <td>{k.name}</td>
                  <td className="mono">{k.prefix}…</td>
                  <td>{new Date(k.createdAt).toLocaleString()}</td>
                  <td style={{ textAlign: "right" }}>
                    <button className="btn sm danger" disabled={busy} onClick={() => revoke(k.id)}>
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <div className="empty">no API keys</div>
        )}
      </div>
    </>
  );
}
