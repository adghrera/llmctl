import { useEffect, useState } from "react";
import { api, MemoryEntry } from "../api/client";
import { ok, err } from "../ui/toast";

export default function Memory() {
  const [entries, setEntries] = useState<MemoryEntry[]>([]);
  const [content, setContent] = useState("");
  const [tags, setTags] = useState("");
  const [source, setSource] = useState("manual");
  const [busy, setBusy] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<MemoryEntry[]>([]);
  const [searching, setSearching] = useState(false);

  const load = () =>
    api
      .memory()
      .then(setEntries)
      .catch((e) => err(e instanceof Error ? e.message : String(e)));

  useEffect(() => {
    load();
  }, []);

  const create = async () => {
    if (!content.trim()) {
      err("content is required");
      return;
    }
    setBusy(true);
    try {
      await api.createMemory({
        content,
        tags: tags.split(",").map((t) => t.trim()).filter(Boolean),
        source,
      });
      ok("memory entry created");
      setContent("");
      setTags("");
      setSource("manual");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (e: MemoryEntry) => {
    if (!window.confirm(`Delete memory entry: "${e.entry.content.slice(0, 60)}..."?`)) return;
    setBusy(true);
    try {
      await api.deleteMemory(e.id);
      ok("memory entry deleted");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const search = async () => {
    if (!searchQuery.trim()) {
      setSearchResults([]);
      return;
    }
    setSearching(true);
    try {
      const results = await api.searchMemory(searchQuery);
      setSearchResults(results);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
      setSearchResults([]);
    } finally {
      setSearching(false);
    }
  };

  return (
    <>
      <div className="card">
        <h2>Agent memory</h2>
        <p style={{ opacity: 0.75, margin: "4px 0 10px" }}>
          Persistent memory entries the agent can recall during chats. Entries
          are searched by keyword and injected as context when relevant.
        </p>
        <div className="row" style={{ gap: 8, marginBottom: 8 }}>
          <input
            style={{ flex: 1, minWidth: 200 }}
            placeholder="Memory content"
            value={content}
            onChange={(e) => setContent(e.target.value)}
          />
          <input
            style={{ flex: 1, minWidth: 140 }}
            placeholder="tags (comma-separated)"
            value={tags}
            onChange={(e) => setTags(e.target.value)}
          />
          <input
            style={{ flex: 1, minWidth: 120 }}
            placeholder="source"
            value={source}
            onChange={(e) => setSource(e.target.value)}
          />
          <button
            className="btn primary"
            disabled={busy || !content.trim()}
            onClick={create}
          >
            {busy ? "Adding…" : "Add memory"}
          </button>
        </div>
      </div>

      <div className="card">
        <h2>Search memory</h2>
        <div className="row" style={{ gap: 8, marginBottom: 8 }}>
          <input
            style={{ flex: 1, minWidth: 200 }}
            placeholder="Search memory entries…"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && search()}
          />
          <button className="btn sm" disabled={searching || !searchQuery.trim()} onClick={search}>
            {searching ? "Searching…" : "Search"}
          </button>
          <button
            className="btn sm"
            disabled={!searchQuery.trim()}
            onClick={() => {
              setSearchQuery("");
              setSearchResults([]);
            }}
          >
            Clear
          </button>
        </div>
        {searchQuery.trim() && searchResults.length === 0 && !searching && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No memory entries matched "{searchQuery}"
          </div>
        )}
        {searchQuery.trim() && searching && (
          <div className="empty muted" style={{ padding: "10px 0" }}>Searching…</div>
        )}
        {searchQuery.trim() && searchResults.length > 0 && (
          <div style={{ marginTop: 8 }}>
            <p style={{ fontSize: 12, opacity: 0.7, marginBottom: 6 }}>
              Memory results for "{searchQuery}" ({searchResults.length})
            </p>
            <table>
              <tbody>
                {searchResults.map((e) => (
                  <tr key={e.id}>
                    <td style={{ width: "70%" }}>
                      <div style={{ fontSize: 13, whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
                        {e.entry.content}
                      </div>
                      <div style={{ fontSize: 11, opacity: 0.6, marginTop: 4 }}>
                        {e.entry.tags && e.entry.tags.length > 0
                          ? e.entry.tags.join(", ")
                          : "no tags"}
                      </div>
                    </td>
                    <td style={{ textAlign: "right" }}>
                      <button
                        className="btn sm danger"
                        onClick={() => remove(e)}
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {!searchQuery.trim() && (
          <>
            <h2 style={{ margin: "16px 0 8px 0" }}>All memory entries ({entries.length})</h2>
            {entries.length === 0 && (
              <div className="empty muted" style={{ padding: "10px 0" }}>
                No memory entries yet. Add one above.
              </div>
            )}
            {entries.length > 0 && (
              <table>
                <tbody>
                  {entries.map((e) => (
                    <tr key={e.id}>
                      <td style={{ width: "70%" }}>
                        <div style={{ fontSize: 13, whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
                          {e.entry.content}
                        </div>
                        <div style={{ fontSize: 11, opacity: 0.6, marginTop: 4 }}>
                          {e.entry.tags && e.entry.tags.length > 0
                            ? e.entry.tags.join(", ")
                            : "no tags"} · {e.entry.source || "manual"}
                        </div>
                      </td>
                      <td style={{ textAlign: "right" }}>
                        <button
                          className="btn sm danger"
                          onClick={() => remove(e)}
                        >
                          Delete
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        )}
      </div>
    </>
  );
}
