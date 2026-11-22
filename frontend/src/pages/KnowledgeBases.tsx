import { useEffect, useRef, useState } from "react";
import { api, KB, KBDoc, KBSearchResult, KBAllSearchResult } from "../api/client";
import { ok, err } from "../ui/toast";

export default function KnowledgeBases() {
  const [kbs, setKBs] = useState<KB[]>([]);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [newTags, setNewTags] = useState(""); // tags at creation (F702)
  const [tagFilter, setTagFilter] = useState(""); // tag filter, CLI `kb list -tag` twin (F707)
  const [file, setFile] = useState<File | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const [busy, setBusy] = useState(false);
  const [selectedKB, setSelectedKB] = useState<KB | null>(null);
  const [docContent, setDocContent] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<KBSearchResult[]>([]);
  const [searching, setSearching] = useState(false);
  const [embedUrl, setEmbedUrl] = useState("");
  const [embedModel, setEmbedModel] = useState("");
  const [embedKey, setEmbedKey] = useState("");
  const [embedBusy, setEmbedBusy] = useState(false);
  const [allQuery, setAllQuery] = useState("");
  const [allResults, setAllResults] = useState<KBAllSearchResult[]>([]);
  const [allSearching, setAllSearching] = useState(false);
  const importRef = useRef<HTMLInputElement>(null);

  const kbTags = Array.from(new Set(kbs.flatMap((k) => k.kb.tags ?? []))).sort();
  const shownKBs = tagFilter
    ? kbs.filter((k) => (k.kb.tags ?? []).includes(tagFilter))
    : kbs;

  const load = () =>
    api
      .knowledgeBases()
      .then(setKBs)
      .catch((e) => err(e instanceof Error ? e.message : String(e)));

  useEffect(() => {
    load();
  }, []);

  const createKB = async () => {
    if (!name.trim()) {
      err("name is required");
      return;
    }
    setBusy(true);
    try {
      await api.createKB({
        name,
        description,
        tags: newTags.split(",").map((s) => s.trim()).filter(Boolean),
      });
      ok("knowledge base created");
      setName("");
      setDescription("");
      setNewTags("");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const updateKB = async (kb: KB) => {
    const newName = prompt("New name:", kb.kb.name);
    if (newName === null) return;
    const newDesc = prompt("New description:", kb.kb.description || "");
    if (newDesc === null) return;
    setBusy(true);
    try {
      await api.updateKB(kb.id, { name: newName, description: newDesc });
      ok("knowledge base updated");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const setTags = async (kb: KB) => {
    const next = window.prompt(`Tags for "${kb.kb.name}" (comma-separated, empty clears):`, (kb.kb.tags ?? []).join(", "));
    if (next === null) return;
    const tags = next.split(",").map((t) => t.trim()).filter(Boolean);
    setBusy(true);
    try {
      await api.setKBTags(kb.id, tags);
      ok(tags.length ? `tags on ${kb.kb.name}: ${tags.join(", ")}` : `tags cleared on ${kb.kb.name}`);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const searchAll = async () => {
    if (!allQuery.trim()) return;
    setAllSearching(true);
    try {
      const r = await api.searchAllKBs({ q: allQuery.trim(), limit: 10 });
      setAllResults(r.results ?? []);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setAllSearching(false);
    }
  };

  const deleteKB = async (kb: KB) => {
    if (!window.confirm(`Delete knowledge base "${kb.kb.name}" and all its documents?`)) return;
    setBusy(true);
    try {
      await api.deleteKB(kb.id);
      ok("knowledge base deleted");
      setSelectedKB(null);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files?.[0]) {
      setFile(e.target.files[0]);
    }
  };

  // Export every document of the selected KB as one JSON array
  // [{filename, content}] — the UI counterpart of `llmctl kb export` (F435).
  const exportKB = async () => {
    if (!selectedKB) return;
    setBusy(true);
    try {
      const docs = await api.listKBDocs(selectedKB.id);
      const items = [];
      for (const d of docs) {
        try {
          items.push({ filename: d.filename, content: await api.getKBDocContent(selectedKB.id, d.id) });
        } catch {
          /* skip unreadable doc, like the CLI does */
        }
      }
      const blob = new Blob([JSON.stringify(items, null, 2) + "\n"], { type: "application/json" });
      const a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = `llmctl-kb-${selectedKB.id}.json`;
      a.click();
      URL.revokeObjectURL(a.href);
      ok(`exported ${items.length} document(s)`);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // Import a JSON array [{filename, content}] produced by exportKB: each
  // entry is uploaded as a new document (F435).
  const importKB = async (f: File) => {
    if (!selectedKB) return;
    setBusy(true);
    try {
      const list = JSON.parse(await f.text());
      if (!Array.isArray(list)) throw new Error("expected a JSON array of {filename, content}");
      let created = 0;
      let skipped = 0;
      for (const it of list) {
        if (!it || typeof it.filename !== "string" || typeof it.content !== "string") {
          skipped++;
          continue;
        }
        try {
          const fd = new FormData();
          fd.append("file", new Blob([it.content], { type: "text/plain" }), it.filename);
          await api.uploadKBDoc(selectedKB.id, fd);
          created++;
        } catch {
          skipped++;
        }
      }
      ok(`imported ${created}, skipped ${skipped}`);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const uploadDoc = async (dropped?: File) => {
    const f = dropped ?? file;
    if (!f || !selectedKB) return;
    setBusy(true);
    try {
      const formData = new FormData();
      formData.append("file", f);
      await api.uploadKBDoc(selectedKB.id, formData);
      ok("document uploaded");
      setFile(null);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const deleteDoc = async (kbId: string, docId: string) => {
    if (!window.confirm("Delete this document?")) return;
    setBusy(true);
    try {
      await api.deleteKBDoc(kbId, docId);
      ok("document deleted");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const viewDoc = async (doc: KBDoc) => {
    if (!selectedKB) return;
    setBusy(true);
    try {
      const content = await api.getKBDocContent(selectedKB.id, doc.id);
      setDocContent(content);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const searchDocs = async () => {
    if (!selectedKB || !searchQuery.trim()) {
      setSearchResults([]);
      return;
    }
    setSearching(true);
    try {
      const results = await api.searchKBDocs(selectedKB.id, { q: searchQuery, limit: 10 });
      setSearchResults(results);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
      setSearchResults([]);
    } finally {
      setSearching(false);
    }
  };

  const saveEmbed = async () => {
    if (!selectedKB) return;
    if (!embedUrl.trim() || !embedModel.trim()) {
      err("embedding url and model are required");
      return;
    }
    setEmbedBusy(true);
    try {
      const rec = await api.setKBEmbed(selectedKB.id, {
        url: embedUrl.trim(),
        model: embedModel.trim(),
        apiKey: embedKey.trim() || undefined,
      });
      ok("semantic search enabled — documents embedded");
      setSelectedKB(rec);
      setEmbedUrl("");
      setEmbedModel("");
      setEmbedKey("");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setEmbedBusy(false);
    }
  };

  const clearEmbed = async () => {
    if (!selectedKB) return;
    setEmbedBusy(true);
    try {
      const rec = await api.setKBEmbed(selectedKB.id, {});
      ok("semantic search disabled — using keyword (TF-IDF) search");
      setSelectedKB(rec);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setEmbedBusy(false);
    }
  };

  return (
    <div>
      <div className="card">
        <h2>Knowledge Bases</h2>
        <p style={{ opacity: 0.75, margin: "4px 0 10px" }}>
          Knowledge bases store documents that can be attached to chat for
          RAG (Retrieval-Augmented Generation). Upload text-based files and
          the agent can reference their content.
        </p>
        <div className="row" style={{ gap: 8, marginBottom: 8 }}>
          <input
            style={{ flex: 1, minWidth: 200 }}
            placeholder="Knowledge base name"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <input
            style={{ flex: 1, minWidth: 200 }}
            placeholder="Description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
          <input
            style={{ width: 140 }}
            placeholder="tags (a,b,c)"
            title="comma-separated tags (CLI: llmctl kb create -tags, F702)"
            value={newTags}
            onChange={(e) => setNewTags(e.target.value)}
          />
          <button
            className="btn primary"
            disabled={busy || !name.trim()}
            onClick={createKB}
          >
            {busy ? "Creating…" : "Create KB"}
          </button>
        </div>
      </div>

      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Knowledge Bases ({kbs.length})</h2>
          <div className="row" style={{ gap: 6 }}>
          {kbTags.length > 0 && (
            <select
              className="oc-input"
              style={{ width: "auto", fontSize: 12 }}
              title="filter by tag — CLI: llmctl kb list -tag (F707)"
              value={tagFilter}
              onChange={(e) => setTagFilter(e.target.value)}
            >
              <option value="">all tags</option>
              {kbTags.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          )}
          <button
            className="btn sm"
            disabled={kbs.length === 0}
            onClick={() => {
              // Same columns as `llmctl kb list --csv` (F393, tags F664).
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "id,name,docs,description,tags",
                ...kbs.map((k) =>
                  [k.id, k.kb.name, String(k.docs?.length ?? 0), k.kb.description || "", (k.kb.tags ?? []).join(";")]
                    .map(esc)
                    .join(",")
                ),
              ];
              const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-kb-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export CSV
          </button>
          <button
            className="btn sm"
            disabled={kbs.length === 0}
            title="download the knowledge-base list as JSON (CLI: llmctl kb list -json)"
            onClick={() => {
              const blob = new Blob([JSON.stringify(kbs, null, 2)], { type: "application/json" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-kb-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export JSON
          </button>
          </div>
        </div>
        {kbs.length === 0 && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No knowledge bases yet. Create one above.
          </div>
        )}
        {kbs.length > 0 && (
          <div>
            <div className="row" style={{ gap: 8, margin: "10px 0 4px" }}>
              <input
                style={{ flex: 1, minWidth: 200 }}
                placeholder="Search all knowledge bases…"
                value={allQuery}
                onChange={(e) => setAllQuery(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && searchAll()}
              />
              <button className="btn sm" disabled={allSearching || !allQuery.trim()} onClick={searchAll}>
                {allSearching ? "Searching…" : "Search all"}
              </button>
            </div>
            {allResults.length > 0 && (
              <div style={{ fontSize: 13 }}>
                {allResults.map((r, i) => (
                  <div key={i} style={{ borderTop: "1px solid var(--border, #333)", padding: "6px 0" }}>
                    <div>
                      <b>{r.kb_name}</b>
                      <span className="muted">
                        {" "}· {r.filename} · chunk {r.chunk_index} · {r.score.toFixed(3)}
                      </span>
                    </div>
                    <div className="muted" style={{ fontSize: 12 }}>
                      {r.chunk_text.replace(/\s+/g, " ").slice(0, 160)}
                    </div>
                  </div>
                ))}
              </div>
            )}
            {allQuery.trim() && !allSearching && allResults.length === 0 && (
              <div className="empty muted" style={{ padding: "6px 0" }}>No chunks matched "{allQuery}"</div>
            )}
          </div>
        )}
        {shownKBs.map((kb) => (
          <div
            key={kb.id}
            style={{
              borderTop: "1px solid var(--border, #333)",
              padding: "12px 0",
              display: "flex",
              flexDirection: "column",
              gap: 8,
            }}
          >
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
              <div>
                <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
                  <b>{kb.kb.name}</b>
                  {kb.kb.embed?.model && (
                    <span
                      style={{ fontSize: 10, opacity: 0.8, border: "1px solid var(--border, #333)", borderRadius: 3, padding: "1px 5px" }}
                      title={`semantic search via ${kb.kb.embed.model}`}
                    >
                      semantic
                    </span>
                  )}
                  <span style={{ opacity: 0.6, fontSize: 12 }}>{kb.kb.id}</span>
                </div>
                {(kb.kb.tags?.length ?? 0) > 0 && (
                  <div style={{ display: "flex", flexWrap: "wrap", gap: 3, marginTop: 2 }}>
                    {kb.kb.tags!.map((t) => (
                      <span key={t} className="badge">{t}</span>
                    ))}
                  </div>
                )}
                {kb.kb.description && (
                  <div style={{ opacity: 0.75, fontSize: 13 }}>{kb.kb.description}</div>
                )}
                <div style={{ fontSize: 12, opacity: 0.7, marginTop: 4 }}>
                  {kb.kb.doc_count} document(s) &middot;{" "}
                  {new Date(kb.kb.updated_at).toLocaleString()}
                </div>
              </div>
              <div className="row" style={{ gap: 4 }}>
                <button className="btn sm" onClick={() => updateKB(kb)}>
                  Edit
                </button>
                <button className="btn sm" title="comma-separated tags (CLI: llmctl kb tag)" onClick={() => setTags(kb)}>
                  Tags
                </button>
                <button
                  className="btn sm"
                  onClick={() => {
                    setSelectedKB(kb);
                    setDocContent(null);
                    setSearchResults([]);
                  }}
                >
                  View Documents
                </button>
                <button className="btn sm danger" onClick={() => deleteKB(kb)}>
                  Delete
                </button>
              </div>
            </div>
          </div>
        ))}
      </div>

      {selectedKB && (
        <div
          className="card"
          // Drop a file anywhere on the KB card to upload it as a document (F453).
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={(e) => {
            e.preventDefault();
            setDragOver(false);
            const f = e.dataTransfer.files?.[0];
            if (f) void uploadDoc(f);
          }}
          style={dragOver ? { outline: "2px dashed var(--primary, #888)", outlineOffset: -2 } : undefined}
        >
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
            <h2 style={{ margin: 0 }}>{selectedKB.kb.name}</h2>
            <div className="row" style={{ gap: 6 }}>
              <button className="btn sm" disabled={busy} onClick={() => void exportKB()}>
                Export JSON
              </button>
              <button className="btn sm" disabled={busy} onClick={() => importRef.current?.click()}>
                Import JSON
              </button>
              <input
                ref={importRef}
                type="file"
                accept="application/json,.json"
                style={{ display: "none" }}
                onChange={(e) => {
                  const f = e.target.files?.[0];
                  if (f) void importKB(f);
                  e.target.value = "";
                }}
              />
              <button className="btn sm" onClick={() => setSelectedKB(null)}>
                Back
              </button>
            </div>
          </div>
          <div className="row" style={{ gap: 8, marginBottom: 12 }}>
            <input
              type="file"
              onChange={handleFileChange}
              disabled={busy}
            />
            {file && (
              <span style={{ fontSize: 12, opacity: 0.7 }}>
                {file.name} ({(file.size / 1024).toFixed(1)} KB)
              </span>
            )}
            <button className="btn primary" disabled={busy || !file} onClick={() => void uploadDoc()}>
              {busy ? "Uploading…" : "Upload Document"}
            </button>
          </div>
          <p className="muted small" style={{ margin: "0 0 10px" }}>
            Text files are stored as-is; PDF and DOCX files have their text
            extracted automatically (PDF best-effort).
          </p>
          <div
            style={{
              border: "1px solid var(--border, #333)",
              borderRadius: 4,
              padding: 8,
              marginBottom: 12,
              fontSize: 13,
            }}
          >
            {selectedKB.kb.embed?.model ? (
              <div className="row" style={{ gap: 8, alignItems: "center", flexWrap: "wrap" }}>
                <span style={{ opacity: 0.85 }}>
                  Semantic search: <b>{selectedKB.kb.embed.model}</b>
                  <span style={{ opacity: 0.6, marginLeft: 6, fontSize: 11 }}>
                    {selectedKB.kb.embed.url}
                  </span>
                </span>
                <button className="btn sm" disabled={embedBusy} onClick={clearEmbed}>
                  {embedBusy ? "Working…" : "Disable"}
                </button>
              </div>
            ) : (
              <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                <span style={{ opacity: 0.75, fontSize: 12 }}>
                  Optional: enable embedding-based semantic search with any
                  OpenAI-compatible <code>/v1/embeddings</code> endpoint (e.g. a
                  running instance). Falls back to keyword search when unset.
                </span>
                <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
                  <input
                    style={{ flex: 2, minWidth: 220 }}
                    placeholder="Embeddings URL (e.g. http://127.0.0.1:8080/v1)"
                    value={embedUrl}
                    onChange={(e) => setEmbedUrl(e.target.value)}
                  />
                  <input
                    style={{ flex: 1, minWidth: 140 }}
                    placeholder="Embedding model"
                    value={embedModel}
                    onChange={(e) => setEmbedModel(e.target.value)}
                  />
                  <input
                    style={{ flex: 1, minWidth: 120 }}
                    placeholder="API key (optional)"
                    type="password"
                    value={embedKey}
                    onChange={(e) => setEmbedKey(e.target.value)}
                  />
                  <button
                    className="btn sm primary"
                    disabled={embedBusy || !embedUrl.trim() || !embedModel.trim()}
                    onClick={saveEmbed}
                  >
                    {embedBusy ? "Embedding…" : "Enable semantic search"}
                  </button>
                </div>
              </div>
            )}
          </div>
          <div className="row" style={{ gap: 8, marginBottom: 8 }}>
            <input
              style={{ flex: 1, minWidth: 200 }}
              placeholder="Search documents…"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && searchDocs()}
            />
            <button className="btn sm" disabled={searching || !searchQuery.trim()} onClick={searchDocs}>
              {searching ? "Searching…" : "Search"}
            </button>
          </div>
          {searchQuery.trim() && searching && (
            <div className="empty muted" style={{ padding: "10px 0" }}>Searching…</div>
          )}
          {searchQuery.trim() && searchResults.length === 0 && !searching && (
            <div className="empty muted" style={{ padding: "10px 0" }}>
              No documents matched "{searchQuery}"
            </div>
          )}
          {searchResults.length > 0 && (
            <div style={{ marginTop: 8 }}>
              <p style={{ fontSize: 12, opacity: 0.7, marginBottom: 6 }}>
                Search results for "{searchQuery}" ({searchResults.length})
              </p>
              {searchResults.map((r, i) => (
                <div key={i} style={{ border: "1px solid var(--border, #333)", borderRadius: 4, padding: 8, marginBottom: 8 }}>
                  <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 4 }}>
                    <b style={{ fontSize: 13 }}>{r.filename}</b>
                    <span style={{ fontSize: 11, opacity: 0.7 }}>score: {r.score.toFixed(4)}</span>
                  </div>
                  <pre style={{ fontSize: 11, whiteSpace: "pre-wrap", margin: 0, opacity: 0.85, maxHeight: 120, overflow: "auto" }}>
                    {r.chunk_text}
                  </pre>
                </div>
              ))}
            </div>
          )}
          {selectedKB.docs.length > 0 && (
            <table>
              <thead>
                <tr>
                  <th>Filename</th>
                  <th>Type</th>
                  <th>Size</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {selectedKB.docs.map((doc) => (
                  <tr key={doc.id}>
                    <td>{doc.filename}</td>
                    <td style={{ fontSize: 12, opacity: 0.7 }}>{doc.content_type}</td>
                    <td style={{ fontSize: 12, opacity: 0.7 }}>
                      {(doc.size / 1024).toFixed(1)} KB
                    </td>
                    <td style={{ textAlign: "right" }}>
                      <button className="btn sm" onClick={() => viewDoc(doc)}>
                        View
                      </button>{" "}
                      <button className="btn sm danger" onClick={() => deleteDoc(selectedKB.id, doc.id)}>
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {docContent && (
        <div className="card">
          <h2>Document Content</h2>
          <button className="btn sm" onClick={() => setDocContent(null)} style={{ marginBottom: 8 }}>
            Close
          </button>
          <pre
            style={{
              padding: 8,
              fontSize: 11,
              fontFamily: "monospace",
              whiteSpace: "pre-wrap",
              background: "rgba(127,127,127,0.08)",
              borderRadius: 4,
              border: "1px solid var(--border, #333)",
              maxHeight: 400,
              overflow: "auto",
            }}
          >
            {docContent}
          </pre>
        </div>
      )}
    </div>
  );
}
