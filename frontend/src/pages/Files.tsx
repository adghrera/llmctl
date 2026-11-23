import { useEffect, useState } from "react";
import { api, FileEntry } from "../api/client";
import { ok, err } from "../ui/toast";

export default function Files() {
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [contentView, setContentView] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [tagFilter, setTagFilter] = useState(""); // tag filter, CLI -tag twin (F700)
  const [sortKey, setSortKey] = useState(""); // column sort over the file list (F778)
  const [sortDir, setSortDir] = useState<1 | -1>(1);
  const [showStats, setShowStats] = useState(false); // per-content-type rollup (CLI `files -stats` parity, F637)
  const [dragOver, setDragOver] = useState(false);

  const load = () =>
    api
      .files()
      .then(setFiles)
      .catch((e) => err(e instanceof Error ? e.message : String(e)));

  useEffect(() => {
    load();
  }, []);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files?.[0]) {
      setFile(e.target.files[0]);
    }
  };

  const upload = async (dropped?: File) => {
    const f = dropped ?? file;
    if (!f) return;
    setBusy(true);
    try {
      const formData = new FormData();
      formData.append("file", f);
      await api.uploadFile(formData);
      ok("file uploaded");
      setFile(null);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (f: FileEntry) => {
    if (!window.confirm(`Delete file: "${f.entry.filename}"?`)) return;
    setBusy(true);
    try {
      await api.deleteFile(f.id);
      ok("file deleted");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // Files uploaded 30+ days ago — UI twin of `llmctl files prune -older-than 30 -yes` (F844).
  const idleCutoff = Date.now() - 30 * 86400000;
  const idleFiles = files.filter((f) => new Date(f.entry.created_at).getTime() < idleCutoff);
  const pruneIdle = async () => {
    if (!window.confirm(`Remove ${idleFiles.length} file(s) uploaded 30+ days ago?`)) return;
    setBusy(true);
    try {
      for (const f of idleFiles) await api.deleteFile(f.id);
      ok(`pruned ${idleFiles.length} old file(s)`);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // Free-form tags, comma-separated in the prompt (F700); empty clears.
  const tagFile = async (f: FileEntry) => {
    const next = window.prompt(`Tags for "${f.entry.filename}" (comma-separated, empty clears):`, (f.entry.tags ?? []).join(", "));
    if (next == null) return;
    const tags = next.split(",").map((t) => t.trim()).filter(Boolean);
    try {
      await api.setFileTags(f.id, tags);
      ok(tags.length ? `tags set: ${tags.join(", ")}` : "tags cleared");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const viewContent = async (f: FileEntry) => {
    if (!isTextFile(f.entry.contentType, f.entry.filename)) {
      err("file is not text-based");
      return;
    }
    setBusy(true);
    try {
      const content = await api.getFileContent(f.id);
      setContentView(content);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  // Filename text filter (case-insensitive substring) for the files table,
  // matching the other list-page filters (F356), plus the tag filter (F700).
  const low = filter.trim().toLowerCase();
  const fileTags = Array.from(new Set(files.flatMap((f) => f.entry.tags ?? []))).sort();
  let shown = low ? files.filter((f) => f.entry.filename.toLowerCase().includes(low)) : files;
  if (tagFilter) shown = shown.filter((f) => (f.entry.tags ?? []).includes(tagFilter));
  const toggleSort = (k: string) => {
    if (sortKey === k) setSortDir((d) => (d === 1 ? -1 : 1));
    else {
      setSortKey(k);
      setSortDir(k === "size" ? -1 : 1);
    }
  };
  const sortedShown = (() => {
    if (!sortKey) return shown;
    const val = (f: FileEntry): string | number => {
      switch (sortKey) {
        case "name": return f.entry.filename || "";
        case "type": return f.entry.contentType || "";
        case "size": return f.entry.size || 0;
        default: return 0;
      }
    };
    return [...shown].sort((a, b) => {
      const va = val(a);
      const vb = val(b);
      const c = typeof va === "number" && typeof vb === "number" ? va - vb : String(va).localeCompare(String(vb));
      return c * sortDir;
    });
  })();
  const arrow = (k: string) => (sortKey === k ? (sortDir === 1 ? " ▲" : " ▼") : "");

  return (
    <>
      <div
        className="card"
        // Drop a file anywhere on the card to upload it (F452).
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragOver(false);
          const f = e.dataTransfer.files?.[0];
          if (f) void upload(f);
        }}
        style={dragOver ? { outline: "2px dashed var(--primary, #888)", outlineOffset: -2 } : undefined}
      >
        <h2>Upload a file</h2>
        <p style={{ opacity: 0.75, margin: "4px 0 10px" }}>
          Upload text-based files (.txt, .md, .py, .js, .json, etc.) to reference
          in chat using <code>/file &lt;filename&gt;</code>. Max 10 MiB. You can
          also drop a file onto this card.
        </p>
        <div className="row" style={{ gap: 8, marginBottom: 8 }}>
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
          <button
            className="btn primary"
            disabled={busy || !file}
            onClick={() => void upload()}
          >
            {busy ? "Uploading…" : "Upload"}
          </button>
        </div>
      </div>

      <div className="card">
        <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>
            Uploaded files ({shown.length}
            {low ? ` of ${files.length}` : ""})
          </h2>
          {files.length > 0 && (
            <input
              className="oc-input"
              style={{ width: 180 }}
              placeholder="filter filename…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
          )}
          {fileTags.length > 0 && (
            <select
              className="oc-input"
              style={{ width: "auto", fontSize: 12 }}
              title="filter by tag — CLI: llmctl files list -tag (F700)"
              value={tagFilter}
              onChange={(e) => setTagFilter(e.target.value)}
            >
              <option value="">all tags</option>
              {fileTags.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          )}
          <button
            className="btn sm"
            aria-pressed={showStats}
            disabled={shown.length === 0}
            title="group files per content type — CLI: llmctl files -stats (F637)"
            onClick={() => setShowStats(!showStats)}
          >
            {showStats ? "Show list" : "Stats"}
          </button>
          <button
            className="btn sm"
            disabled={shown.length === 0}
            onClick={() => {
              // Same columns as `llmctl files list --csv` (F397); filter-aware.
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "id,filename,content_type,size_bytes,source,tags,created_at",
                ...shown.map((f) =>
                  [
                    f.entry.id, f.entry.filename, f.entry.contentType,
                    String(f.entry.size), f.entry.source, (f.entry.tags ?? []).join(" "), f.entry.created_at,
                  ]
                    .map(esc)
                    .join(",")
                ),
              ];
              const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-files-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export CSV
          </button>
          <button
            className="btn sm"
            disabled={files.length === 0}
            title="download the file list as JSON (CLI: llmctl files -json)"
            onClick={() => {
              const blob = new Blob([JSON.stringify(files, null, 2)], { type: "application/json" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-files-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export JSON
          </button>
          {idleFiles.length > 0 && (
            <button
              className="btn sm danger"
              disabled={busy}
              title={`remove ${idleFiles.length} file(s) uploaded 30+ days ago (CLI: llmctl files prune -older-than 30 -yes)`}
              onClick={pruneIdle}
            >
              Prune old ({idleFiles.length})
            </button>
          )}
        </div>
        {files.length === 0 && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No files uploaded yet.
          </div>
        )}
        {files.length > 0 && showStats && (
          <table>
            <thead>
              <tr>
                <th>Content type</th>
                <th>Files</th>
                <th>Bytes</th>
                <th>Last</th>
              </tr>
            </thead>
            <tbody>
              {(() => {
                // Same rollup as `llmctl files -stats` (F637): one row per
                // content type, sorted by total bytes.
                const by = new Map<string, { files: number; bytes: number; last: string }>();
                for (const f of shown) {
                  const ct = f.entry.contentType || "(none)";
                  const g = by.get(ct) ?? { files: 0, bytes: 0, last: "" };
                  g.files++;
                  g.bytes += f.entry.size ?? 0;
                  if ((f.entry.created_at || "") > g.last) g.last = f.entry.created_at || "";
                  by.set(ct, g);
                }
                return [...by.entries()]
                  .sort((a, b) => b[1].bytes - a[1].bytes)
                  .map(([ct, g]) => (
                    <tr key={ct}>
                      <td>{ct}</td>
                      <td>{g.files}</td>
                      <td>{(g.bytes / 1024).toFixed(1)} KB</td>
                      <td className="muted" style={{ fontSize: 12 }}>
                        {g.last ? new Date(g.last).toLocaleString() : ""}
                      </td>
                    </tr>
                  ));
              })()}
            </tbody>
          </table>
        )}
        {files.length > 0 && !showStats && (
          <table>
            <thead>
              <tr>
                <th style={{ cursor: "pointer" }} title="sort by filename (F778)" onClick={() => toggleSort("name")}>Filename{arrow("name")}</th>
                <th style={{ cursor: "pointer" }} title="sort by content type (F778)" onClick={() => toggleSort("type")}>Type{arrow("type")}</th>
                <th style={{ cursor: "pointer" }} title="sort by size (F778)" onClick={() => toggleSort("size")}>Size{arrow("size")}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {sortedShown.map((f) => (
                <tr key={f.id}>
                  <td>
                    <b>{f.entry.filename}</b>
                    {(f.entry.tags ?? []).map((t) => (
                      <span
                        key={t}
                        style={{
                          marginLeft: 6,
                          fontSize: 10,
                          opacity: 0.8,
                          background: "rgba(120,200,120,0.15)",
                          padding: "2px 6px",
                          borderRadius: 4,
                        }}
                      >
                        {t}
                      </span>
                    ))}
                  </td>
                  <td style={{ fontSize: 12, opacity: 0.7 }}>
                    {f.entry.contentType}
                  </td>
                  <td style={{ fontSize: 12, opacity: 0.7 }}>
                    {(f.entry.size / 1024).toFixed(1)} KB
                  </td>
                  <td style={{ textAlign: "right" }}>
                    {isTextFile(f.entry.contentType, f.entry.filename) && (
                      <button
                        className="btn sm"
                        onClick={() => viewContent(f)}
                      >
                        View
                      </button>
                    )}
                    <a
                      className="btn sm"
                      href={`/api/v1/files/${encodeURIComponent(f.id)}/content`}
                      download={f.entry.filename}
                      title="download the stored bytes (CLI: llmctl files cat -out)"
                    >
                      Download
                    </a>
                    <button
                      className="btn sm"
                      title="edit tags (CLI: llmctl files tag)"
                      onClick={() => tagFile(f)}
                    >
                      Tag
                    </button>
                    <button
                      className="btn sm danger"
                      onClick={() => remove(f)}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {files.length > 0 && shown.length === 0 && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No files match “{filter.trim()}”.
          </div>
        )}
      </div>

      {contentView && (
        <div className="card">
          <h2>File content</h2>
          <button
            className="btn sm"
            onClick={() => setContentView(null)}
            style={{ marginBottom: 8, marginRight: 6 }}
          >
            Close
          </button>
          <button
            className="btn sm"
            style={{ marginBottom: 8 }}
            title="copy the file content to the clipboard (F761)"
            onClick={() => {
              navigator.clipboard
                ?.writeText(contentView)
                .then(() => ok("copied"))
                .catch(() => {});
            }}
          >
            Copy
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
            {contentView}
          </pre>
        </div>
      )}
    </>
  );
}

function isTextFile(contentType: string, filename: string): boolean {
  const ct = contentType.toLowerCase();
  if (ct.startsWith("text/")) return true;
  if (ct === "application/json" || ct === "application/javascript" || ct === "application/xml") return true;
  const ext = filename.toLowerCase().substring(filename.lastIndexOf("."));
  const textExts = [".txt", ".md", ".py", ".js", ".ts", ".json", ".xml", ".html", ".css", ".go", ".rs", ".java", ".c", ".h", ".yml", ".yaml", ".toml", ".ini", ".cfg", ".conf", ".log"];
  return textExts.includes(ext);
}
