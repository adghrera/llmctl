import { useEffect, useState } from "react";
import { api, KB, KBDoc } from "../api/client";
import { ok, err } from "../ui/toast";

export default function KnowledgeBases() {
  const [kbs, setKBs] = useState<KB[]>([]);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [selectedKB, setSelectedKB] = useState<KB | null>(null);
  const [docContent, setDocContent] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<KBDoc[]>([]);
  const [searching, setSearching] = useState(false);

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
      await api.createKB({ name, description });
      ok("knowledge base created");
      setName("");
      setDescription("");
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

  const uploadDoc = async () => {
    if (!file || !selectedKB) return;
    setBusy(true);
    try {
      const formData = new FormData();
      formData.append("file", file);
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
        <h2>Knowledge Bases ({kbs.length})</h2>
        {kbs.length === 0 && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No knowledge bases yet. Create one above.
          </div>
        )}
        {kbs.map((kb) => (
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
                  <span style={{ opacity: 0.6, fontSize: 12 }}>{kb.kb.id}</span>
                </div>
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
        <div className="card">
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
            <h2 style={{ margin: 0 }}>{selectedKB.kb.name}</h2>
            <button className="btn sm" onClick={() => setSelectedKB(null)}>
              Back
            </button>
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
            <button className="btn primary" disabled={busy || !file} onClick={uploadDoc}>
              {busy ? "Uploading…" : "Upload Document"}
            </button>
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
