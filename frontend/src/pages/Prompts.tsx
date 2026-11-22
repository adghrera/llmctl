import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api, PromptTemplate } from "../api/client";
import { ok, err } from "../ui/toast";

export default function Prompts() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [templates, setTemplates] = useState<PromptTemplate[]>([]);
  const [categories, setCategories] = useState<string[]>([]);
  const [selectedCategory, setSelectedCategory] = useState("");
  const [filter, setFilter] = useState(params.get("q") || "");
  const [copied, setCopied] = useState<Set<string>>(new Set());
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [nf, setNf] = useState({ id: "", name: "", category: "", description: "", content: "" });
  // Inline render preview (CLI `prompts render -var` parity, F361).
  const [render, setRender] = useState<{ id: string; input: string; vars: string; out: string } | null>(null);

  const doRender = (t: PromptTemplate, input: string, vars: string): string => {
    let out = t.content.split("{{input}}").join(input);
    for (const kv of vars.split(",").map((s) => s.trim()).filter(Boolean)) {
      const i = kv.indexOf("=");
      if (i > 0) out = out.split(`{{${kv.slice(0, i).trim()}}}`).join(kv.slice(i + 1));
    }
    return out;
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

  const filtered = templates.filter((t) => {
    if (selectedCategory && t.category !== selectedCategory) return false;
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
        <h2>Templates ({filtered.length})</h2>
        {filtered.length === 0 && (
          <div className="empty muted" style={{ padding: "10px 0" }}>
            No templates in this category.
          </div>
        )}
        {filtered.map((t) => (
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
              onClick={() => {
                sessionStorage.setItem("llmctl.chatSeed", t.content);
                navigate("/chat");
              }}
            >
              Insert into chat
            </button>{" "}
            <button
              className="btn sm"
              onClick={() =>
                setRender(
                  render?.id === t.id
                    ? null
                    : { id: t.id, input: "", vars: "", out: t.content }
                )
              }
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
              </div>
            )}
            {t.custom && (
              <>
                <button className="btn sm" onClick={() => startEdit(t)}>
                  Edit
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
