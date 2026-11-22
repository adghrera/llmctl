import { useEffect, useState } from "react";
import { api, FileEntry } from "../api/client";
import { ok, err } from "../ui/toast";

export default function Files() {
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [contentView, setContentView] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
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
  // matching the other list-page filters (F356).
  const low = filter.trim().toLowerCase();
  const shown = low ? files.filter((f) => f.entry.filename.toLowerCase().includes(low)) : files;

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
          <button
            className="btn sm"
            disabled={shown.length === 0}
            onClick={() => {
              // Same columns as `llmctl files list --csv` (F397); filter-aware.
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "id,filename,content_type,size_bytes,source,created_at",
                ...shown.map((f) =>
                  [
                    f.entry.id, f.entry.filename, f.entry.contentType,
                    String(f.entry.size), f.entry.source, f.entry.created_at,
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
        </div>
        {files.length === 0 && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No files uploaded yet.
          </div>
        )}
        {files.length > 0 && (
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
              {shown.map((f) => (
                <tr key={f.id}>
                  <td>
                    <b>{f.entry.filename}</b>
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
            style={{ marginBottom: 8 }}
          >
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
