import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, type ApiKey } from "../api/client";
import { useStore } from "../state/store";
import { ok, err } from "../ui/toast";

export default function Keys() {
  const { keys, models: allModels, refreshOne } = useStore();
  const [name, setName] = useState("");
  const [quota, setQuota] = useState(0);
  const [models, setModels] = useState("");
  const [conc, setConc] = useState(0);
  const [maxReq, setMaxReq] = useState(0);
  const [rps, setRps] = useState(0);
  const [burst, setBurst] = useState(0);
  const [expiresDays, setExpiresDays] = useState(0);
  const [budget, setBudget] = useState(0);
  const [renameFor, setRenameFor] = useState<string | null>(null);
  const [renameDraft, setRenameDraft] = useState("");
  const [quotaFor, setQuotaFor] = useState<string | null>(null);
  const [quotaTok, setQuotaTok] = useState("");
  const [quotaReqs, setQuotaReqs] = useState("");
  const [modelsFor, setModelsFor] = useState<string | null>(null);
  const [modelsDraft, setModelsDraft] = useState<string[]>([]);
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
      const r = await api.createKey(name, quota, allowed, conc, maxReq, rps, burst, expiresDays, budget);
      setCreated(r.key);
      setName("");
      setQuota(0);
      setModels("");
      setConc(0);
      setMaxReq(0);
      setRps(0);
      setBurst(0);
      setExpiresDays(0);
      setBudget(0);
      ok(`created key ${r.prefix}…`);
      await refreshOne("keys");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const saveModels = async (id: string) => {
    if (busy) return;
    setBusy(true);
    try {
      await api.updateKey(id, { allowedModels: modelsDraft });
      ok(modelsDraft.length ? "model allowlist updated" : "key may use all models");
      setModelsFor(null);
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

  const pruneExpired = async () => {
    setBusy(true);
    try {
      const r = await api.pruneKeys();
      ok(`removed ${r.removed.length} expired key(s)`);
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

  const renameKey = async (k: ApiKey) => {
    const name = renameDraft.trim();
    if (!name || busy) return;
    setBusy(true);
    try {
      await api.updateKey(k.id, { allowedModels: k.allowedModels ?? [], name });
      ok("key renamed");
      setRenameFor(null);
      await refreshOne("keys");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const saveQuota = async (k: ApiKey) => {
    if (busy) return;
    const tok = quotaTok.trim() === "" ? 0 : Number(quotaTok);
    const reqs = quotaReqs.trim() === "" ? 0 : Number(quotaReqs);
    if (Number.isNaN(tok) || Number.isNaN(reqs) || tok < 0 || reqs < 0) {
      err("quotas must be >= 0 (empty = unlimited)");
      return;
    }
    setBusy(true);
    try {
      await api.updateKey(k.id, { allowedModels: k.allowedModels ?? [], tokenQuota: tok, maxRequests: reqs });
      ok("quotas updated");
      setQuotaFor(null);
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
          <input
            type="number"
            min={0}
            step={0.1}
            style={{ width: 90 }}
            placeholder="rps"
            title="Sustained requests/sec limit (0 = gateway default)"
            value={rps || ""}
            onChange={(e) => setRps(Math.max(0, Number(e.target.value) || 0))}
          />
          <input
            type="number"
            min={0}
            style={{ width: 90 }}
            placeholder="burst"
            title="Request burst size (0 = gateway default)"
            value={burst || ""}
            onChange={(e) => setBurst(Math.max(0, Number(e.target.value) || 0))}
          />
          <input
            type="number"
            min={0}
            style={{ width: 100 }}
            placeholder="days"
            title="Key expires after this many days (0 = never)"
            value={expiresDays || ""}
            onChange={(e) => setExpiresDays(Math.max(0, Number(e.target.value) || 0))}
          />
          <input
            type="number"
            min={0}
            step={0.01}
            style={{ width: 90 }}
            placeholder="$ / day"
            title="Daily cost budget in USD (0 = unlimited)"
            value={budget || ""}
            onChange={(e) => setBudget(Math.max(0, Number(e.target.value) || 0))}
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
        {keys.some((k) => k.expiresAt && new Date(k.expiresAt).getTime() < Date.now()) && (
          <p style={{ margin: "0 0 10px" }}>
            <button className="btn sm danger" disabled={busy} onClick={pruneExpired}>
              Prune expired
            </button>{" "}
            <span className="muted small">
              {keys.filter((k) => k.expiresAt && new Date(k.expiresAt).getTime() < Date.now()).length} key(s) past expiry
            </span>
          </p>
        )}
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
                  <td>
                    {renameFor === k.id ? (
                      <span style={{ display: "inline-flex", gap: 4, alignItems: "center" }}>
                        <input
                          autoFocus
                          style={{ width: 130 }}
                          value={renameDraft}
                          onChange={(e) => setRenameDraft(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") renameKey(k);
                            if (e.key === "Escape") setRenameFor(null);
                          }}
                        />
                        <button className="btn sm primary" disabled={busy} onClick={() => renameKey(k)}>
                          Save
                        </button>
                        <button className="btn sm" onClick={() => setRenameFor(null)}>
                          ✕
                        </button>
                      </span>
                    ) : (
                      <span>
                        {k.name}{" "}
                        <button
                          className="btn sm"
                          title="rename key"
                          style={{ opacity: 0.6 }}
                          onClick={() => {
                            setRenameFor(k.id);
                            setRenameDraft(k.name);
                          }}
                        >
                          ✎
                        </button>
                      </span>
                    )}
                  </td>
                  <td className="mono">{k.prefix}…</td>
                  <td>
                    {new Date(k.createdAt).toLocaleString()}
                    {k.lastUsed && (
                      <div className="muted" style={{ fontSize: 11 }}>used {new Date(k.lastUsed).toLocaleString()}</div>
                    )}
                    {k.expiresAt &&
                      (new Date(k.expiresAt).getTime() < Date.now() ? (
                        <div className="danger" style={{ fontSize: 11 }}>expired {new Date(k.expiresAt).toLocaleString()}</div>
                      ) : (
                        <div className="muted" style={{ fontSize: 11 }}>expires {new Date(k.expiresAt).toLocaleDateString()}</div>
                      ))}
                  </td>
                  <td className="mono">
                    {quotaFor === k.id ? (
                      <span style={{ display: "inline-flex", gap: 4, alignItems: "center", flexWrap: "wrap" }}>
                        <input
                          autoFocus
                          style={{ width: 90 }}
                          placeholder="tok quota"
                          value={quotaTok}
                          onChange={(e) => setQuotaTok(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") saveQuota(k);
                            if (e.key === "Escape") setQuotaFor(null);
                          }}
                        />
                        <input
                          style={{ width: 80 }}
                          placeholder="reqs"
                          value={quotaReqs}
                          onChange={(e) => setQuotaReqs(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") saveQuota(k);
                            if (e.key === "Escape") setQuotaFor(null);
                          }}
                        />
                        <button className="btn sm primary" disabled={busy} onClick={() => saveQuota(k)}>
                          Save
                        </button>
                        <button className="btn sm" onClick={() => setQuotaFor(null)}>
                          ✕
                        </button>
                      </span>
                    ) : (
                      <span>
                        {k.usedTokens.toLocaleString()} / {k.tokenQuota ? k.tokenQuota.toLocaleString() : "∞"}
                        {k.usedCost ? <span className="muted"> · ${k.usedCost.toFixed(4)}</span> : null}
                        {k.maxRequests ? (
                          <span className="muted"> · {k.usedRequests ?? 0}/{k.maxRequests} reqs</span>
                        ) : null}
                        {k.rateRPS ? (
                          <span className="muted" title={`rate limit: ${k.rateRPS} req/s (burst ${k.rateBurst || "default"})`}>
                            {" "}{k.rateRPS}/s
                          </span>
                        ) : null}{" "}
                        <button
                          className="btn sm"
                          title="edit quotas"
                          style={{ opacity: 0.6 }}
                          onClick={() => {
                            setQuotaFor(k.id);
                            setQuotaTok(k.tokenQuota ? String(k.tokenQuota) : "");
                            setQuotaReqs(k.maxRequests ? String(k.maxRequests) : "");
                          }}
                        >
                          ✎
                        </button>
                      </span>
                    )}
                  </td>
                  <td className="mono">
                    {(() => {
                      const u = today[k.id];
                      const cost = u?.cost ?? 0;
                      const budgetStr = k.dailyCostBudgetUSD ? ` / $${k.dailyCostBudgetUSD}` : "";
                      if (!u) return k.dailyCostBudgetUSD ? `$0${budgetStr}` : "—";
                      return `${(u.prompt + u.comp).toLocaleString()} tok · ${u.requests} req${cost ? ` · $${cost.toFixed(4)}${budgetStr}` : ""}`;
                    })()}
                  </td>
                  <td className="muted" style={{ fontSize: 12 }}>
                    {modelsFor === k.id ? (
                      <span style={{ display: "inline-flex", flexDirection: "column", gap: 2 }}>
                        {Array.from(new Set([...allModels.map((m) => m.id), ...modelsDraft])).map((mid) => (
                          <label key={mid} style={{ display: "inline-flex", gap: 4, alignItems: "center", whiteSpace: "nowrap" }}>
                            <input
                              type="checkbox"
                              checked={modelsDraft.includes(mid)}
                              onChange={(e) =>
                                setModelsDraft((d) => (e.target.checked ? [...d, mid] : d.filter((x) => x !== mid)))
                              }
                            />
                            {mid}
                          </label>
                        ))}
                        <span style={{ display: "inline-flex", gap: 4, marginTop: 2 }}>
                          <button className="btn sm primary" disabled={busy} onClick={() => saveModels(k.id)}>
                            Save
                          </button>
                          <button className="btn sm" onClick={() => setModelsFor(null)}>
                            ✕
                          </button>
                          <span className="muted" style={{ alignSelf: "center" }}>
                            {modelsDraft.length ? "" : "empty = all"}
                          </span>
                        </span>
                      </span>
                    ) : k.allowedModels?.length ? (
                      k.allowedModels.join(", ")
                    ) : (
                      "all"
                    )}
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
                    <button
                      className="btn sm"
                      style={{ marginRight: 6 }}
                      disabled={busy}
                      onClick={() => {
                        setModelsFor(k.id);
                        setModelsDraft(k.allowedModels ?? []);
                      }}
                    >
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
