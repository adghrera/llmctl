import { useState, useEffect } from "react";
import { api, type CatalogEntry } from "../api/client";
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

  const installedIds = new Set(models.map((m) => m.id));

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
                    {m.running ? (
                      <span className="badge run">running</span>
                    ) : m.downloaded ? (
                      <span className="badge ok">ready</span>
                    ) : (
                      <span className="badge">downloading</span>
                    )}
                  </td>
                  <td style={{ textAlign: "right" }}>
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
    </>
  );
}
