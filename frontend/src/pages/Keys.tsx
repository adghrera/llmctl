import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import { useStore } from "../state/store";
import { ok, err } from "../ui/toast";

export default function Keys() {
  const { keys, refreshOne } = useStore();
  const [name, setName] = useState("");
  const [quota, setQuota] = useState(0);
  const [models, setModels] = useState("");
  const [conc, setConc] = useState(0);
  const [maxReq, setMaxReq] = useState(0);
  const [created, setCreated] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [today, setToday] = useState<Record<string, { requests: number; prompt: number; comp: number; cost?: number }>>({});

  useEffect(() => {
    let alive = true;
    const tick = () => {
      const day = new Date().toISOString().slice(0, 10);
      api
        .dailyKeyUsage(2)
        .then((d) => alive && setToday(d[day] ?? {}))
        .catch(() => {});
    };
    tick();
    const t = setInterval(tick, 15000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);

  const create = async () => {
    setBusy(true);
    try {
      const allowed = models.split(",").map((s) => s.trim()).filter(Boolean);
      const r = await api.createKey(name, quota, allowed, conc, maxReq);
      setCreated(r.key);
      setName("");
      setQuota(0);
      setModels("");
      setConc(0);
      setMaxReq(0);
      ok(`created key ${r.prefix}…`);
      await refreshOne("keys");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const editModels = async (id: string, current?: string[]) => {
    const input = window.prompt(
      "Allowed models (comma-separated, empty = all):",
      (current ?? []).join(", ")
    );
    if (input === null) return;
    setBusy(true);
    try {
      await api.updateKey(id, input.split(",").map((s) => s.trim()).filter(Boolean));
      ok("model allowlist updated");
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

  const resetUsage = async (id: string) => {
    setBusy(true);
    try {
      await api.resetKeyUsage(id);
      ok("usage counters reset");
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
          <input
            type="number"
            min={0}
            step={1000}
            style={{ width: 140 }}
            placeholder="token quota"
            title="Total token quota (0 = unlimited)"
            value={quota || ""}
            onChange={(e) => setQuota(Math.max(0, Number(e.target.value) || 0))}
          />
          <input
            style={{ width: 200 }}
            placeholder="allowed models (csv, empty = all)"
            title="Comma-separated model ids this key may call; empty = all"
            value={models}
            onChange={(e) => setModels(e.target.value)}
          />
          <input
            type="number"
            min={0}
            style={{ width: 110 }}
            placeholder="max conc."
            title="Max simultaneous in-flight requests (0 = unlimited)"
            value={conc || ""}
            onChange={(e) => setConc(Math.max(0, Number(e.target.value) || 0))}
          />
          <input
            type="number"
            min={0}
            style={{ width: 110 }}
            placeholder="max reqs"
            title="Total request cap (0 = unlimited)"
            value={maxReq || ""}
            onChange={(e) => setMaxReq(Math.max(0, Number(e.target.value) || 0))}
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
                <th>Used / quota</th>
                <th>Today</th>
                <th>Models</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id}>
                  <td>{k.name}</td>
                  <td className="mono">{k.prefix}…</td>
                  <td>{new Date(k.createdAt).toLocaleString()}</td>
                  <td className="mono">
                    {k.usedTokens.toLocaleString()} / {k.tokenQuota ? k.tokenQuota.toLocaleString() : "∞"}
                    {k.usedCost ? <span className="muted"> · ${k.usedCost.toFixed(4)}</span> : null}
                    {k.maxRequests ? (
                      <span className="muted"> · {k.usedRequests ?? 0}/{k.maxRequests} reqs</span>
                    ) : null}
                  </td>
                  <td className="mono">
                    {(() => {
                      const u = today[k.id];
                      return u
                        ? `${(u.prompt + u.comp).toLocaleString()} tok · ${u.requests} req${u.cost ? ` · $${u.cost.toFixed(4)}` : ""}`
                        : "—";
                    })()}
                  </td>
                  <td className="muted" style={{ fontSize: 12 }}>
                    {k.allowedModels?.length ? k.allowedModels.join(", ") : "all"}
                  </td>
                  <td style={{ textAlign: "right" }}>
                    <Link
                      className="btn sm"
                      style={{ marginRight: 6 }}
                      to={`/requests?key=${encodeURIComponent(k.id)}`}
                      title="recent requests by this key"
                    >
                      Log
                    </Link>
                    <button className="btn sm" style={{ marginRight: 6 }} disabled={busy} onClick={() => editModels(k.id, k.allowedModels)}>
                      Models
                    </button>
                    <button
                      className="btn sm"
                      style={{ marginRight: 6 }}
                      disabled={busy || k.usedTokens === 0}
                      title="reset usage counters (tokens, requests, cost)"
                      onClick={() => resetUsage(k.id)}
                    >
                      Reset
                    </button>
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
