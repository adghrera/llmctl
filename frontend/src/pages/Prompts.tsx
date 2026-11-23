import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api, chatStream, PromptTemplate } from "../api/client";
import { ok, err } from "../ui/toast";

export default function Prompts() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [templates, setTemplates] = useState<PromptTemplate[]>([]);
  const [categories, setCategories] = useState<string[]>([]);
  const [selectedCategory, setSelectedCategory] = useState("");
  const [selectedTag, setSelectedTag] = useState(""); // tag filter, CLI -tag twin (F695)
  const [filter, setFilter] = useState(params.get("q") || "");
  const [copied, setCopied] = useState<Set<string>>(new Set());
  const [showStats, setShowStats] = useState(false); // per-category rollup (CLI `prompts -stats` parity, F636)
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [nf, setNf] = useState({ id: "", name: "", category: "", description: "", content: "" });
  // Inline render preview (CLI `prompts render -var` parity, F361).
  const [render, setRender] = useState<{ id: string; input: string; vars: string; out: string } | null>(null);
  // Run the rendered prompt through the gateway (CLI `prompts run` parity, F596).
  const [runModel, setRunModel] = useState("");
  const [runReply, setRunReply] = useState<{ id: string; text: string; busy: boolean } | null>(null);
  const [modelChoices, setModelChoices] = useState<string[]>([]);

  const doRender = (t: PromptTemplate, input: string, vars: string): string => {
    let out = t.content.split("{{input}}").join(input);
    for (const kv of vars.split(",").map((s) => s.trim()).filter(Boolean)) {
      const i = kv.indexOf("=");
      if (i > 0) out = out.split(`{{${kv.slice(0, i).trim()}}}`).join(kv.slice(i + 1));
    }
    return out;
  };

  const runPrompt = async (t: PromptTemplate, rendered: string) => {
    if (!runModel) {
      err("pick a model first");
      return;
    }
    setRunReply({ id: t.id, text: "", busy: true });
    try {
      const key = localStorage.getItem("llmctl.chat.key") ?? "";
      for await (const delta of chatStream(key, runModel, [{ role: "user", content: rendered }])) {
        setRunReply((r) => (r ? { ...r, text: r.text + delta } : r));
      }
      setRunReply((r) => (r ? { ...r, busy: false } : r));
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
      setRunReply((r) => (r ? { ...r, busy: false } : r));
    }
  };

  const saveTemplate = async () => {
    if (editing) {
      if (!nf.name || !nf.content) {
        err("name and content are required");
        return;
      }
      setBusy(true);
      try {
        await api.updatePrompt(editing, {
          name: nf.name,
          category: nf.category,
          description: nf.description,
          content: nf.content,
        });
        ok(`template ${editing} updated`);
        closeForm();
        await load();
      } catch (e) {
        err(e instanceof Error ? e.message : String(e));
      }
      setBusy(false);
      return;
    }
    if (!nf.id || !nf.name || !nf.content) {
      err("id, name and content are required");
      return;
    }
    setBusy(true);
    try {
      await api.createPrompt(nf);
      ok(`template ${nf.id} created`);
      closeForm();
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
    setBusy(false);
  };

  const closeForm = () => {
    setAdding(false);
    setEditing(null);
    setNf({ id: "", name: "", category: "", description: "", content: "" });
  };

  const startEdit = (t: PromptTemplate) => {
    setEditing(t.id);
    setNf({ id: t.id, name: t.name, category: t.category, description: t.description, content: t.content });
    setAdding(true);
  };

  const deleteTemplate = async (id: string) => {
    if (!window.confirm(`Delete custom prompt ${id}?`)) return;
    try {
      await api.deletePrompt(id);
      ok("template deleted");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  // Free-form tags on custom templates, comma-separated in the prompt
  // (F694); empty clears. Twin of the Scheduler page Tag button.
  const tagTemplate = async (t: PromptTemplate) => {
    const next = window.prompt(`Tags for "${t.name}" (comma-separated, empty clears):`, (t.tags ?? []).join(", "));
    if (next == null) return;
    const tags = next.split(",").map((s) => s.trim()).filter(Boolean);
    try {
      await api.setPromptTags(t.id, tags);
      ok(tags.length ? `tags set: ${tags.join(", ")}` : "tags cleared");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const duplicateTemplate = async (t: PromptTemplate) => {
    // Same flow as `llmctl prompts duplicate` (F638): copy any template to a
    // new custom id so it can be edited without touching the original.
    const newId = window.prompt(`Duplicate ${t.id} as (new id):`, `${t.id}-copy`);
    if (!newId || !newId.trim()) return;
    try {
      await api.createPrompt({
        id: newId.trim(),
        name: `${t.name} copy`,
        category: t.category,
        description: t.description,
        content: t.content,
        tags: t.tags, // copies tags too (F703)
      });
      ok(`template ${newId.trim()} created`);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const retagAll = async () => {
    // Bulk-rename a category across custom templates — UI twin of
    // `llmctl prompts retag` (F644). Built-ins are skipped (read-only).
    const oldCat = window.prompt("Rename which category? (custom templates only)");
    if (!oldCat || !oldCat.trim()) return;
    const newCat = (window.prompt(`New category for "${oldCat.trim()}":`) ?? "").trim();
    if (!newCat) return;
    let changed = 0;
    try {
      for (const t of templates) {
        if (!t.custom || t.category !== oldCat.trim()) continue;
        await api.updatePrompt(t.id, {
          name: t.name, category: newCat, description: t.description, content: t.content,
        });
        changed++;
      }
      ok(`retagged ${changed} template(s)`);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const load = async () => {
    try {
      const [t, c] = await Promise.all([api.prompts(), api.promptCategories()]);
      setTemplates(t);
      setCategories(c);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  useEffect(() => {
    load();
  }, []);

  // JSON export/import, same shapes as `llmctl prompts export|import` (F409).
  const importFileRef = useRef<HTMLInputElement>(null);
  const exportJson = () => {
    const recs = templates
      .filter((t) => t.custom)
      .map((t) => ({ id: t.id, name: t.name, description: t.description, category: t.category, content: t.content }));
    const blob = new Blob([JSON.stringify(recs, null, 2) + "\n"], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `llmctl-prompts-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  };
  const importJson = async (f: File) => {
    try {
      const entries = JSON.parse(await f.text()) as Array<Record<string, string>>;
      if (!Array.isArray(entries)) throw new Error("expected a JSON array");
      let created = 0;
      let skipped = 0;
      for (const e of entries) {
        try {
          await api.createPrompt({
            id: String(e.id ?? ""), name: String(e.name ?? ""),
            description: e.description, category: e.category, content: String(e.content ?? ""),
          });
          created++;
        } catch {
          skipped++;
        }
      }
      ok(`imported ${created} prompts (${skipped} skipped)`);
      load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const filtered = templates.filter((t) => {
    if (selectedCategory && t.category !== selectedCategory) return false;
    if (selectedTag && !(t.tags ?? []).includes(selectedTag)) return false;
    const low = filter.trim().toLowerCase();
    if (!low) return true;
    return (
      t.id.toLowerCase().includes(low) ||
      t.name.toLowerCase().includes(low) ||
      (t.description || "").toLowerCase().includes(low) ||
      (t.category || "").toLowerCase().includes(low) ||
      t.content.toLowerCase().includes(low)
    );
  });

  const copyToClipboard = async (content: string, id: string) => {
    try {
      await navigator.clipboard.writeText(content);
      setCopied((prev) => {
        const next = new Set(prev);
        next.add(id);
        setTimeout(() => next.delete(id), 1500);
        return next;
      });
      ok("Copied to clipboard");
    } catch {
      err("Failed to copy to clipboard");
    }
  };

  return (
    <>
      <div className="card">
        <h2>Prompt Templates</h2>
        <p style={{ opacity: 0.75, margin: "4px 0 10px" }}>
          Curated prompt templates you can insert into chat. Click "Insert into
          chat" to open the Chat page with the template in the input, or "Copy"
          to copy a template to your clipboard.
        </p>
        <input
          className="input"
          placeholder="Filter by name, description or content…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          style={{ width: "100%", marginBottom: 10 }}
        />
        <div className="row" style={{ gap: 8, marginBottom: 12 }}>
          <span style={{ fontSize: 12, opacity: 0.7 }}>Category:</span>{" "}
          {categories.map((cat) => (
            <button
              key={cat}
              className={`btn sm ${selectedCategory === cat ? "primary" : ""}`}
              onClick={() =>
                setSelectedCategory(selectedCategory === cat ? "" : cat)
              }
            >
              {cat}
            </button>
          ))}
          <button
            className="btn sm"
            onClick={() => setSelectedCategory("")}
            style={{ opacity: 0.7 }}
          >
            All
          </button>
          {new Set(templates.flatMap((t) => t.tags ?? [])).size > 0 && (
            <select
              className="input"
              style={{ width: "auto", fontSize: 12 }}
              title="filter by tag — CLI: llmctl prompts -tag (F695)"
              value={selectedTag}
              onChange={(e) => setSelectedTag(e.target.value)}
            >
              <option value="">all tags</option>
              {Array.from(new Set(templates.flatMap((t) => t.tags ?? [])))
                .sort()
                .map((tag) => (
                  <option key={tag} value={tag}>
                    {tag}
                  </option>
                ))}
            </select>
          )}
          <button className="btn sm primary" onClick={() => (adding ? closeForm() : setAdding(true))}>
            {adding ? "Cancel" : "New template"}
          </button>
        </div>
      </div>

      {adding && (
        <div className="card">
          <h2>{editing ? `Edit template ${editing}` : "New custom template"}</h2>
          <div className="row" style={{ gap: 8, flexWrap: "wrap", marginBottom: 8 }}>
            <input
              className="input"
              placeholder="id (lowercase, dashes)"
              value={nf.id}
              disabled={!!editing}
              onChange={(e) => setNf({ ...nf, id: e.target.value })}
              style={{ width: 180 }}
            />
            <input
              className="input"
              placeholder="name"
              value={nf.name}
              onChange={(e) => setNf({ ...nf, name: e.target.value })}
              style={{ width: 180 }}
            />
            <input
              className="input"
              placeholder="category (optional)"
              value={nf.category}
              onChange={(e) => setNf({ ...nf, category: e.target.value })}
              style={{ width: 160 }}
            />
            <input
              className="input"
              placeholder="description (optional)"
              value={nf.description}
              onChange={(e) => setNf({ ...nf, description: e.target.value })}
              style={{ width: 240 }}
            />
          </div>
          <textarea
            className="input"
            placeholder="template text — use {{input}} as the placeholder"
            value={nf.content}
            onChange={(e) => setNf({ ...nf, content: e.target.value })}
            rows={4}
            style={{ width: "100%", fontFamily: "monospace", fontSize: 12, marginBottom: 8 }}
          />
          <button className="btn primary" disabled={busy} onClick={saveTemplate}>
            {editing ? "Save changes" : "Create template"}
          </button>
        </div>
      )}

      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Templates ({filtered.length})</h2>
          <div className="row" style={{ gap: 6 }}>
          <button
            className="btn sm"
            aria-pressed={showStats}
            disabled={filtered.length === 0}
            title="group templates per category — CLI: llmctl prompts -stats (F636)"
            onClick={() => setShowStats(!showStats)}
          >
            {showStats ? "Show list" : "Stats"}
          </button>
          <button
            className="btn sm"
            disabled={templates.filter((t) => t.custom).length === 0}
            title="rename a category across custom templates — CLI: llmctl prompts retag (F644)"
            onClick={() => void retagAll()}
          >
            Retag
          </button>
          <button
            className="btn sm"
            disabled={filtered.length === 0}
            onClick={() => {
              // Same columns as `llmctl prompts list --csv` (F394).
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "id,category,name,description",
                ...filtered.map((t) =>
                  [t.id, t.category, t.name, t.description].map(esc).join(",")
                ),
              ];
              const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-prompts-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export CSV
          </button>
          <button
            className="btn sm"
            disabled={!templates.some((t) => t.custom)}
            title="download custom templates as JSON (CLI: llmctl prompts export)"
            onClick={exportJson}
          >
            Export JSON
          </button>
          <button
            className="btn sm"
            title="import a prompts JSON file (CLI: llmctl prompts import)"
            onClick={() => importFileRef.current?.click()}
          >
            Import JSON
          </button>
          <input
            ref={importFileRef}
            type="file"
            accept="application/json,.json"
            style={{ display: "none" }}
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) importJson(f);
              e.target.value = "";
            }}
          />
          </div>
        </div>
        {filtered.length === 0 && !showStats && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No templates in this category.
          </div>
        )}
        {showStats && filtered.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>Category</th>
                <th>Templates</th>
              </tr>
            </thead>
            <tbody>
              {(() => {
                // Same rollup as `llmctl prompts -stats` (F636): one row per
                // category, "(none)" groups uncategorized, sorted by count.
                const by = new Map<string, number>();
                for (const t of filtered) {
                  const c = t.category || "(none)";
                  by.set(c, (by.get(c) ?? 0) + 1);
                }
                return [...by.entries()]
                  .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
                  .map(([cat, n]) => (
                    <tr key={cat}>
                      <td>{cat}</td>
                      <td>{n}</td>
                    </tr>
                  ));
              })()}
            </tbody>
          </table>
        )}
        {!showStats && filtered.map((t) => (
          <div
            key={t.id}
            style={{
              borderTop: "1px solid var(--border, #333)",
              padding: "12px 0",
              display: "flex",
              flexDirection: "column",
              gap: 8,
            }}
          >
            <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
              <b>{t.name}</b>
              <span style={{ opacity: 0.6, fontSize: 12 }}>{t.id}</span>
              <span
                style={{
                  fontSize: 10,
                  opacity: 0.6,
                  background: "rgba(127,127,127,0.1)",
                  padding: "2px 6px",
                  borderRadius: 4,
                }}
              >
                {t.category}
              </span>
              {t.custom && (
                <span
                  style={{
                    fontSize: 10,
                    opacity: 0.8,
                    background: "rgba(80,160,255,0.15)",
                    padding: "2px 6px",
                    borderRadius: 4,
                  }}
                >
                  custom
                </span>
              )}
              {t.tags?.map((tag) => (
                <span
                  key={tag}
                  style={{
                    fontSize: 10,
                    opacity: 0.8,
                    background: "rgba(120,200,120,0.15)",
                    padding: "2px 6px",
                    borderRadius: 4,
                  }}
                >
                  {tag}
                </span>
              ))}
            </div>
            {t.description && (
              <div style={{ opacity: 0.75, fontSize: 13, marginBottom: 6 }}>
                {t.description}
              </div>
            )}
            <pre
              style={{
                margin: 0,
                padding: 8,
                fontSize: 11,
                fontFamily: "monospace",
                whiteSpace: "pre-wrap",
                background: "rgba(127,127,127,0.08)",
                borderRadius: 4,
                border: "1px solid var(--border, #333)",
              }}
            >
              {t.content}
            </pre>
            <button
              className="btn sm"
              disabled={copied.has(t.id)}
              onClick={() => copyToClipboard(t.content, t.id)}
            >
              {copied.has(t.id) ? "Copied ✓" : "Copy to clipboard"}
            </button>{" "}
            <button
              className="btn sm"
              title="copy this template to a new custom id — CLI: llmctl prompts duplicate (F639)"
              onClick={() => duplicateTemplate(t)}
            >
              Duplicate
            </button>{" "}
            <button
              className="btn sm"
              onClick={() => {
                sessionStorage.setItem("llmctl.chatSeed", t.content);
                navigate("/chat");
              }}
            >
              Insert into chat
            </button>{" "}
            <button
              className="btn sm"
              onClick={() => {
                if (render?.id === t.id) {
                  setRender(null);
                  return;
                }
                setRender({ id: t.id, input: "", vars: "", out: t.content });
                if (modelChoices.length === 0) {
                  api.models().then((ms) => setModelChoices(ms.map((m) => m.id))).catch(() => {});
                }
              }}
            >
              {render?.id === t.id ? "Hide render" : "Render"}
            </button>
            {render?.id === t.id && (
              <div
                style={{
                  border: "1px solid var(--border, #333)",
                  borderRadius: 4,
                  padding: 8,
                  display: "flex",
                  flexDirection: "column",
                  gap: 6,
                }}
              >
                <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
                  <input
                    className="input"
                    style={{ flex: 2, minWidth: 160 }}
                    placeholder="{{input}} text"
                    value={render.input}
                    onChange={(e) =>
                      setRender({ ...render, input: e.target.value, out: doRender(t, e.target.value, render.vars) })
                    }
                  />
                  <input
                    className="input"
                    style={{ flex: 1, minWidth: 140 }}
                    placeholder="extra vars: k=v, k2=v2"
                    title="replaces {{k}} with v for each pair"
                    value={render.vars}
                    onChange={(e) =>
                      setRender({ ...render, vars: e.target.value, out: doRender(t, render.input, e.target.value) })
                    }
                  />
                </div>
                <pre
                  style={{
                    margin: 0,
                    padding: 8,
                    fontSize: 11,
                    fontFamily: "monospace",
                    whiteSpace: "pre-wrap",
                    background: "rgba(127,127,127,0.08)",
                    borderRadius: 4,
                  }}
                >
                  {render.out}
                </pre>
                <button
                  className="btn sm"
                  onClick={() => {
                    navigator.clipboard.writeText(render.out);
                    ok("rendered text copied");
                  }}
                >
                  Copy rendered
                </button>
                <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
                  <select
                    className="input"
                    style={{ width: 180 }}
                    title="model to send the rendered prompt to (CLI: llmctl prompts run -model M)"
                    value={runModel}
                    onChange={(e) => setRunModel(e.target.value)}
                  >
                    <option value="">model…</option>
                    {modelChoices.map((m) => (
                      <option key={m} value={m}>{m}</option>
                    ))}
                  </select>
                  <button
                    className="btn sm primary"
                    disabled={runReply?.busy || !runModel || !render.out.trim()}
                    onClick={() => runPrompt(t, render.out)}
                  >
                    {runReply?.busy && runReply.id === t.id ? "Running…" : "Run"}
                  </button>
                  <button
                    className="btn sm"
                    disabled={!runModel || !render.out.trim()}
                    title="copy a curl command for the rendered prompt (Chat Copy cURL twin, F789)"
                    onClick={() => {
                      const body = JSON.stringify({
                        model: runModel,
                        messages: [{ role: "user", content: render.out }],
                        stream: true,
                      });
                      const q = `'${body.replace(/'/g, `'\\''`)}'`;
                      const cmd =
                        `curl -N ${window.location.origin}/v1/chat/completions \\\n` +
                        `  -H "Authorization: Bearer $LLMCTL_API_KEY" \\\n` +
                        `  -H "Content-Type: application/json" \\\n` +
                        `  -d ${q}\n`;
                      navigator.clipboard
                        ?.writeText(cmd)
                        .then(() => ok("copied curl command"))
                        .catch(() => err("clipboard unavailable"));
                    }}
                  >
                    Copy cURL
                  </button>
                </div>
                {runReply?.id === t.id && (runReply.text || runReply.busy) && (
                  <>
                  <pre
                    style={{
                      margin: 0,
                      padding: 8,
                      fontSize: 11,
                      fontFamily: "monospace",
                      whiteSpace: "pre-wrap",
                      background: "rgba(127,127,127,0.08)",
                      borderRadius: 4,
                      maxHeight: 200,
                      overflow: "auto",
                    }}
                  >
                    {runReply.text || "…"}
                  </pre>
                  {!runReply.busy && runReply.text && (
                    <button
                      className="btn sm"
                      style={{ marginTop: 6 }}
                      title="copy the model reply to the clipboard (F773)"
                      onClick={() => {
                        navigator.clipboard
                          ?.writeText(runReply.text)
                          .then(() => ok("copied"))
                          .catch(() => {});
                      }}
                    >
                      Copy reply
                    </button>
                  )}
                  {!runReply.busy && runReply.text && (
                    <button
                      className="btn sm"
                      style={{ marginTop: 6, marginLeft: 6 }}
                      title="save the rendered prompt and reply as a saved chat (CLI: prompts run -save, F839)"
                      onClick={async () => {
                        try {
                          const c = await api.createChat({
                            title: t.name,
                            model: runModel,
                            messages: [
                              { role: "user", content: render.out },
                              { role: "assistant", content: runReply.text },
                            ],
                          });
                          ok(`saved as chat "${c.title}"`);
                        } catch (e) {
                          err(e instanceof Error ? e.message : String(e));
                        }
                      }}
                    >
                      Save to chat
                    </button>
                  )}
                  </>
                )}
              </div>
            )}
            {t.custom && (
              <>
                <button className="btn sm" onClick={() => startEdit(t)}>
                  Edit
                </button>{" "}
                <button
                  className="btn sm"
                  title="edit tags (CLI: llmctl prompts tag)"
                  onClick={() => tagTemplate(t)}
                >
                  Tag
                </button>{" "}
                <button className="btn sm danger" onClick={() => deleteTemplate(t.id)}>
                  Delete
                </button>
              </>
            )}
          </div>
        ))}
      </div>
    </>
  );
}
