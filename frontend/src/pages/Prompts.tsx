import { useEffect, useState } from "react";
import { api, PromptTemplate } from "../api/client";
import { ok, err } from "../ui/toast";

export default function Prompts() {
  const [templates, setTemplates] = useState<PromptTemplate[]>([]);
  const [categories, setCategories] = useState<string[]>([]);
  const [selectedCategory, setSelectedCategory] = useState("");
  const [copied, setCopied] = useState<Set<string>>(new Set());

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

  const filtered = templates.filter(
    (t) => !selectedCategory || t.category === selectedCategory
  );

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
          Curated prompt templates you can insert into chat. Click "Copy" to
          copy a template to your clipboard, then paste it into the chat input.
        </p>
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
        </div>
      </div>

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
            </button>
          </div>
        ))}
      </div>
    </>
  );
}
