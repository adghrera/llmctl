import { useState, useEffect } from "react";
import { api, type CatalogEntry, type ModelMetadata, type ModelAlias } from "../api/client";
import { useStore } from "../state/store";
import { ok, err } from "../ui/toast";

function fmtBytes(n: number): string {
  if (n >= 1e9) return (n / 1e9).toFixed(2) + " GB";
  if (n >= 1e6) return (n / 1e6).toFixed(1) + " MB";
  if (n >= 1e3) return (n / 1e3).toFixed(0) + " KB";
  return n + " B";
}

export default function Models() {
  const { models, backends, refreshOne } = useStore();
  const [catalog, setCatalog] = useState<CatalogEntry[]>([]);
  const [catalogBusy, setCatalogBusy] = useState<string | null>(null);

  useEffect(() => {
    api.catalog().then(setCatalog).catch(() => setCatalog([]));
  }, []);

  const installCatalog = async (entry: CatalogEntry) => {
    setCatalogBusy(entry.id);
    try {
      await api.installCatalogModel(entry.id);
      ok(`installing ${entry.name} (${entry.sizeGB} GB) — see Jobs`);
      await refreshOne("models");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setCatalogBusy(null);
    }
  };

  // Register an existing local model file (F083).
  const [regId, setRegId] = useState("");
  const [regPath, setRegPath] = useState("");
  const [regBackend, setRegBackend] = useState("llama.cpp");
  const [regBusy, setRegBusy] = useState(false);
  const registerLocal = async () => {
    setRegBusy(true);
    try {
      await api.registerModel({ id: regId.trim(), path: regPath.trim(), backend: regBackend });
      ok(`registered ${regId}`);
      setRegId("");
      setRegPath("");
      await refreshOne("models");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setRegBusy(false);
    }
  };

  const [q, setQ] = useState("");
  const [results, setResults] = useState<Array<{ modelId?: string; id?: string; downloads?: number }>>([]);
  const [searched, setSearched] = useState(false);
  const [repo, setRepo] = useState("");
  const [files, setFiles] = useState<Array<{ Path: string; SizeBytes: number }>>([]);
  const [file, setFile] = useState("");
  const [backend, setBackend] = useState("llama.cpp");
  const [busy, setBusy] = useState(false);

  const doSearch = async () => {
    setBusy(true);
    try {
      const r = await api.hfSearch(q);
      setResults(r);
      setSearched(true);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const pickRepo = async (r: string) => {
    setRepo(r);
    setFile("");
    setFiles([]);
    try {
      const f = await api.hfFiles(r);
      setFiles(f);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const install = async () => {
    if (!repo || !file) return;
    setBusy(true);
    try {
      await api.installModel({ repo, file, backend });
      ok(`installing ${repo}/${file}`);
      setRepo("");
      setFile("");
      setFiles([]);
      setResults([]);
      setSearched(false);
      await refreshOne("models");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    setBusy(true);
    try {
      await api.removeModel(id);
      ok(`removed ${id}`);
      await refreshOne("models");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // per-model context size (tokens); 0/empty = backend default
  const [ctxDraft, setCtxDraft] = useState<Record<string, string>>({});
  const saveCtx = async (id: string) => {
    const raw = (ctxDraft[id] ?? "").trim();
    const n = raw === "" ? 0 : Number(raw);
    if (Number.isNaN(n) || n < 0 || n > 1000000) {
      err("ctx must be 0–1000000 (0 = backend default)");
      return;
    }
    setBusy(true);
    try {
      const res = await api.setModelCtx(id, n);
      ok(res.restartRequired ? `ctx ${n || "default"} saved — restart running instance to apply` : `ctx ${n || "default"} saved`);
      setCtxDraft((d) => {
        const c = { ...d };
        delete c[id];
        return c;
      });
      await refreshOne("models");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // per-model idle-unload override: -1 = never, 0/empty = global, >0 = minutes
  const [idleDraft, setIdleDraft] = useState<Record<string, string>>({});
  const saveIdle = async (id: string) => {
    const raw = (idleDraft[id] ?? "").trim();
    const n = raw === "" ? 0 : Number(raw);
    if (Number.isNaN(n) || n < -1 || n > 100000) {
      err("idle must be -1 (never), 0 (global) or minutes");
      return;
    }
    setBusy(true);
    try {
      await api.setModelIdle(id, n);
      ok(n === -1 ? "model pinned (never idle-unloaded)" : n === 0 ? "idle unload follows global setting" : `idle unload after ${n} min for this model`);
      setIdleDraft((d) => {
        const c = { ...d };
        delete c[id];
        return c;
      });
      await refreshOne("models");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // per-model USD price per 1M tokens (in/out); empty = not set
  const [priceDraft, setPriceDraft] = useState<Record<string, { i: string; o: string }>>({});
  const savePrice = async (id: string) => {
    const d = priceDraft[id] ?? { i: "", o: "" };
    const pi = d.i === "" ? 0 : Number(d.i);
    const po = d.o === "" ? 0 : Number(d.o);
    if (Number.isNaN(pi) || Number.isNaN(po) || pi < 0 || po < 0) {
      err("prices must be >= 0 (USD per 1M tokens; empty = 0)");
      return;
    }
    setBusy(true);
    try {
      await api.setModelPrice(id, pi, po);
      ok(pi || po ? `price: $${pi}/M in · $${po}/M out` : "price cleared (free)");
      setPriceDraft((m) => {
        const c = { ...m };
        delete c[id];
        return c;
      });
      await refreshOne("models");
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const installedIds = new Set(models.map((m) => m.id));

  // GGUF metadata viewer
  const [metaFor, setMetaFor] = useState<string | null>(null);
  const [meta, setMeta] = useState<ModelMetadata | null>(null);
  const [metaBusy, setMetaBusy] = useState(false);
  const viewMeta = async (id: string) => {
    if (metaFor === id) {
      setMetaFor(null);
      setMeta(null);
      return;
    }
    setMetaFor(id);
    setMeta(null);
    setMetaBusy(true);
    try {
      setMeta(await api.modelMetadata(id));
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
      setMetaFor(null);
    } finally {
      setMetaBusy(false);
    }
  };

  return (
    <>
      {catalog.length > 0 && (
        <div className="card">
          <h2>Featured models</h2>
          <table>
            <thead>
              <tr>
                <th>Model</th>
                <th>Quant</th>
                <th>Size</th>
                <th>Tags</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {catalog.map((c) => (
                <tr key={c.id}>
                  <td>
                    <div>{c.name}</div>
                    <div className="muted" style={{ fontSize: 12 }}>{c.description}</div>
                  </td>
                  <td className="mono">{c.quant}</td>
                  <td>{c.sizeGB} GB</td>
                  <td className="muted">{(c.tags || []).join(", ")}</td>
                  <td style={{ textAlign: "right" }}>
                    {installedIds.has(c.id) ? (
                      <span className="badge ok">installed</span>
                    ) : (
                      <button
                        className="btn sm primary"
                        disabled={catalogBusy !== null}
                        onClick={() => installCatalog(c)}
                      >
                        {catalogBusy === c.id ? "Starting…" : "Install"}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className="card">
        <h2>Install a model</h2>
        <div className="row">
          <input
            style={{ flex: 1, minWidth: 220 }}
            placeholder="Search HuggingFace (e.g. qwen2.5 1.5b gguf)"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && doSearch()}
          />
          <button className="btn primary" disabled={busy || !q} onClick={doSearch}>
            Search
          </button>
        </div>
        {searched && (
          <div style={{ marginTop: 10 }}>
            {results.length ? (
              <table>
                <thead>
                  <tr>
                    <th>Repository</th>
                    <th>Downloads</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {results.map((r) => {
                    const id = r.modelId || r.id || "";
                    return (
                      <tr key={id}>
                        <td className="mono">{id}</td>
                        <td>{r.downloads?.toLocaleString() ?? ""}</td>
                        <td style={{ textAlign: "right" }}>
                          <button className="btn sm" onClick={() => pickRepo(id)}>
                            Choose file
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            ) : (
              <div className="empty">no results</div>
            )}
          </div>
        )}
        {repo && (
          <div style={{ marginTop: 12 }}>
            <div className="row">
              <div>
                <label>Repository</label>
                <input className="mono" value={repo} onChange={(e) => setRepo(e.target.value)} />
              </div>
              <div style={{ flex: 1 }}>
                <label>GGUF file</label>
                <select value={file} onChange={(e) => setFile(e.target.value)}>
                  <option value="">— select —</option>
                  {files.map((f) => (
                    <option key={f.Path} value={f.Path}>
                      {f.Path} ({fmtBytes(f.SizeBytes)})
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label>Backend</label>
                <select value={backend} onChange={(e) => setBackend(e.target.value)}>
                  {backends.map((b) => (
                    <option key={b.id} value={b.id}>
                      {b.id}
                    </option>
                  ))}
                </select>
              </div>
              <div style={{ alignSelf: "flex-end" }}>
                <button className="btn primary" disabled={busy || !file} onClick={install}>
                  Install
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
      <div className="card" id="register-local">
        <h2>Register local model</h2>
        <div className="row" style={{ gap: 8, flexWrap: "wrap", alignItems: "flex-end" }}>
          <label className="oc-field" style={{ fontSize: 12 }}>
            id
            <input
              className="oc-input"
              style={{ width: 150 }}
              placeholder="my-model"
              value={regId}
              onChange={(e) => setRegId(e.target.value)}
            />
          </label>
          <label className="oc-field" style={{ fontSize: 12, flex: 1, minWidth: 220 }}>
            path to .gguf file
            <input
              className="oc-input"
              placeholder="/path/to/model.gguf"
              value={regPath}
              onChange={(e) => setRegPath(e.target.value)}
            />
          </label>
          <label className="oc-field" style={{ fontSize: 12 }}>
            backend
            <select className="oc-input" value={regBackend} onChange={(e) => setRegBackend(e.target.value)}>
              {backends.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.id}
                </option>
              ))}
            </select>
          </label>
          <button
            className="btn primary"
            disabled={regBusy || !regId.trim() || !regPath.trim()}
            onClick={registerLocal}
          >
            {regBusy ? "Registering…" : "Register"}
          </button>
        </div>
        <p className="muted small" style={{ margin: "8px 0 0" }}>
          Points llmctl at a model file already on disk (no download).
        </p>
      </div>
      <div className="card">
        <h2>Installed models</h2>
        {models.length ? (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Backend</th>
                <th>Size</th>
                <th>Context</th>
                <th>Idle</th>
                <th>Price $/M</th>
                <th>Status</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {models.map((m) => (
                <tr key={m.id}>
                  <td className="mono">{m.id}</td>
                  <td>{m.backendId}</td>
                  <td>{fmtBytes(m.sizeBytes)}</td>
                  <td>
                    <span style={{ display: "inline-flex", gap: 4, alignItems: "center" }}>
                      <input
                        style={{ width: 92 }}
                        list="ctx-sizes"
                        inputMode="numeric"
                        placeholder={m.ctxSize ? String(m.ctxSize) : "default"}
                        value={ctxDraft[m.id] ?? ""}
                        disabled={busy}
                        onChange={(e) => setCtxDraft((d) => ({ ...d, [m.id]: e.target.value }))}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") saveCtx(m.id);
                        }}
                      />
                      {(ctxDraft[m.id] ?? "") !== "" && (
                        <button className="btn sm" disabled={busy} onClick={() => saveCtx(m.id)}>
                          Set
                        </button>
                      )}
                      {!m.ctxSize && (ctxDraft[m.id] ?? "") === "" && <span className="muted">default</span>}
                    </span>
                  </td>
                  <td>
                    <span style={{ display: "inline-flex", gap: 4, alignItems: "center" }}>
                      <input
                        style={{ width: 70 }}
                        inputMode="numeric"
                        placeholder={m.idleUnloadMin === -1 ? "never" : m.idleUnloadMin ? String(m.idleUnloadMin) : "global"}
                        title="Idle-unload minutes for this model: -1 = never (pin), 0 = global setting"
                        value={idleDraft[m.id] ?? ""}
                        disabled={busy}
                        onChange={(e) => setIdleDraft((d) => ({ ...d, [m.id]: e.target.value }))}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") saveIdle(m.id);
                        }}
                      />
                      {(idleDraft[m.id] ?? "") !== "" && (
                        <button className="btn sm" disabled={busy} onClick={() => saveIdle(m.id)}>
                          Set
                        </button>
                      )}
                      {!m.idleUnloadMin && (idleDraft[m.id] ?? "") === "" && <span className="muted">global</span>}
                      {m.idleUnloadMin === -1 && (idleDraft[m.id] ?? "") === "" && <span className="badge">pinned</span>}
                    </span>
                  </td>
                  <td>
                    <span style={{ display: "inline-flex", gap: 4, alignItems: "center" }}>
                      <input
                        style={{ width: 56 }}
                        placeholder={m.priceInPerM ? String(m.priceInPerM) : "in"}
                        title="USD per 1M prompt tokens"
                        value={priceDraft[m.id]?.i ?? ""}
                        disabled={busy}
                        onChange={(e) => setPriceDraft((p) => ({ ...p, [m.id]: { i: e.target.value, o: p[m.id]?.o ?? "" } }))}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") savePrice(m.id);
                        }}
                      />
                      <span className="muted">/</span>
                      <input
                        style={{ width: 56 }}
                        placeholder={m.priceOutPerM ? String(m.priceOutPerM) : "out"}
                        title="USD per 1M completion tokens"
                        value={priceDraft[m.id]?.o ?? ""}
                        disabled={busy}
                        onChange={(e) => setPriceDraft((p) => ({ ...p, [m.id]: { i: p[m.id]?.i ?? "", o: e.target.value } }))}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") savePrice(m.id);
                        }}
                      />
                      {((priceDraft[m.id]?.i ?? "") !== "" || (priceDraft[m.id]?.o ?? "") !== "") && (
                        <button className="btn sm" disabled={busy} onClick={() => savePrice(m.id)}>
                          Set
                        </button>
                      )}
                      {!m.priceInPerM && !m.priceOutPerM && (priceDraft[m.id]?.i ?? "") === "" && (priceDraft[m.id]?.o ?? "") === "" && (
                        <span className="muted">free</span>
                      )}
                    </span>
                  </td>
                  <td>
                    {m.running ? (
                      <span className="badge run">running</span>
                    ) : m.downloaded ? (
                      <span className="badge ok">ready</span>
                    ) : (
                      <span className="badge">downloading</span>
                    )}
                  </td>
                  <td style={{ textAlign: "right" }}>
                    <button className="btn sm" disabled={busy || !m.downloaded} onClick={() => viewMeta(m.id)}>
                      {metaFor === m.id ? "Hide meta" : "Meta"}
                    </button>{" "}
                    <button
                      className="btn sm"
                      disabled={busy}
                      title="pre-fill the register form from this model"
                      onClick={() => {
                        setRegId(`${m.id}-copy`);
                        setRegPath(m.path);
                        setRegBackend(m.backendId);
                        document.getElementById("register-local")?.scrollIntoView({ behavior: "smooth" });
                        ok("register form pre-filled — choose a new id");
                      }}
                    >
                      Dup
                    </button>{" "}
                    <button className="btn sm danger" disabled={busy || m.running} onClick={() => remove(m.id)}>
                      Remove
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <div className="empty">no models installed</div>
        )}
        <datalist id="ctx-sizes">
          {[2048, 4096, 8192, 16384, 32768, 65536, 131072].map((n) => (
            <option key={n} value={n} />
          ))}
        </datalist>
      </div>
      {metaFor && (
        <div className="card">
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
            <h2 style={{ margin: 0 }}>GGUF metadata — {metaFor}</h2>
            <button className="btn sm" onClick={() => { setMetaFor(null); setMeta(null); }}>
              Close
            </button>
          </div>
          {metaBusy && <div className="empty muted">Parsing…</div>}
          {meta && (
            <>
              <div className="row" style={{ gap: 16, flexWrap: "wrap", fontSize: 13, marginBottom: 8 }}>
                <span>arch: <b>{meta.summary.architecture || "?"}</b></span>
                <span>name: <b>{meta.summary.name || "?"}</b></span>
                <span>quant: <b>{meta.summary.quant || "?"}</b></span>
                <span>ctx: <b>{meta.summary.contextLength?.toLocaleString() || "?"}</b></span>
                <span>layers: <b>{meta.summary.blockCount?.toLocaleString() || "?"}</b></span>
                <span>tensors: <b>{meta.nTensors.toLocaleString()}</b></span>
              </div>
              <details>
                <summary style={{ cursor: "pointer", fontSize: 13, opacity: 0.8 }}>
                  All metadata keys ({Object.keys(meta.metadata).length})
                </summary>
                <table style={{ marginTop: 8 }}>
                  <thead>
                    <tr>
                      <th>Key</th>
                      <th>Value</th>
                    </tr>
                  </thead>
                  <tbody>
                    {Object.entries(meta.metadata)
                      .sort(([a], [b]) => a.localeCompare(b))
                      .map(([k, v]) => (
                        <tr key={k}>
                          <td className="mono" style={{ fontSize: 12 }}>{k}</td>
                          <td style={{ fontSize: 12, wordBreak: "break-all" }}>
                            {typeof v === "object" ? JSON.stringify(v) : String(v)}
                          </td>
                        </tr>
                      ))}
                  </tbody>
                </table>
              </details>
            </>
          )}
        </div>
      )}
      <AliasesCard />
    </>
  );
}

function AliasesCard() {
  const { models } = useStore();
  const [aliases, setAliases] = useState<ModelAlias[]>([]);
  const [alias, setAlias] = useState("");
  const [target, setTarget] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => api.aliases().then(setAliases).catch(() => {});
  useEffect(() => {
    load();
  }, []);

  const add = async () => {
    if (!alias.trim() || !target) {
      err("alias and model are required");
      return;
    }
    setBusy(true);
    try {
      await api.createAlias(alias.trim().toLowerCase(), target);
      ok(`alias "${alias.trim().toLowerCase()}" → ${target}`);
      setAlias("");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (a: string) => {
    try {
      await api.deleteAlias(a);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="card">
      <h2>Aliases</h2>
      <p className="muted small" style={{ margin: "4px 0 12px" }}>
        Stable gateway names: send <span className="mono">model="chat"</span> and the
        gateway routes to the mapped model id, so clients survive model swaps.
      </p>
      <div className="row" style={{ gap: 8, marginBottom: 12, flexWrap: "wrap" }}>
        <input
          placeholder="alias (e.g. chat)"
          value={alias}
          onChange={(e) => setAlias(e.target.value)}
          style={{ width: 160 }}
        />
        <select value={target} onChange={(e) => setTarget(e.target.value)}>
          <option value="">— model —</option>
          {models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.id}
            </option>
          ))}
        </select>
        <button className="btn primary" disabled={busy} onClick={add}>
          Add alias
        </button>
      </div>
      {aliases.length === 0 ? (
        <div className="empty">no aliases configured</div>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>Alias</th>
              <th>Model</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {aliases.map((a) => (
              <tr key={a.alias}>
                <td className="mono">{a.alias}</td>
                <td className="mono">{a.model}</td>
                <td style={{ textAlign: "right" }}>
                  <button className="btn sm danger" onClick={() => remove(a.alias)}>
                    Remove
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
