import { useEffect, useState } from "react";
import { api, SkillRecord } from "../api/client";
import { ok, err } from "../ui/toast";

export default function Skills() {
  const inputTmpl = "{{input}}";
  const convTmpl = "{{conversation}}";
  const [skills, setSkills] = useState<SkillRecord[]>([]);
  const [editing, setEditing] = useState<string | null>(null);
  const [newSkill, setNewSkill] = useState({
    short_name: "",
    name: "",
    description: "",
    prompt_template: "",
    tags: "",
  });
  const [editSkill, setEditSkill] = useState({
    short_name: "",
    name: "",
    description: "",
    prompt_template: "",
    tags: "",
  });
  const [busy, setBusy] = useState(false);

  const load = () =>
    api
      .skills()
      .then(setSkills)
      .catch((e) => err(e instanceof Error ? e.message : String(e)));

  useEffect(() => {
    load();
  }, []);

  const create = async () => {
    const tags = newSkill.tags
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean);
    setBusy(true);
    try {
      const rec = await api.createSkill({
        short_name: newSkill.short_name,
        name: newSkill.name || newSkill.short_name,
        description: newSkill.description,
        prompt_template: newSkill.prompt_template,
        tags,
      });
      ok(`created skill "${rec.manifest.name}"`);
      setNewSkill({
        short_name: "",
        name: "",
        description: "",
        prompt_template: "",
        tags: "",
      });
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const update = async (id: string) => {
    const s = editSkill;
    const tags = s.tags.split(",").map((t) => t.trim()).filter(Boolean);
    setBusy(true);
    try {
      await api.updateSkill(id, {
        short_name: s.short_name,
        name: s.name || s.short_name,
        description: s.description,
        prompt_template: s.prompt_template,
        tags,
      });
      ok("skill updated");
      setEditing(null);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const toggleEnabled = async (s: SkillRecord) => {
    const next = !s.enabled;
    setBusy(true);
    try {
      await api.setSkillEnabled(s.id, next);
      ok(`skill ${next ? "enabled" : "disabled"}`);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (s: SkillRecord) => {
    if (!window.confirm(`Remove skill "${s.manifest.name}"?`)) return;
    setBusy(true);
    try {
      await api.deleteSkill(s.id);
      ok(`removed skill "${s.manifest.name}"`);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="card">
        <h2>Create a skill</h2>
        <p style={{ opacity: 0.75, margin: "4px 0 10px" }}>
          A skill is a reusable prompt template invoked in chat with{" "}
          <code>"/s."{"&lt;"}short_name{"&gt;"}&lt;text&gt;</code>. The skill prompt is inserted as a
          user message before your input. Use <code>{inputTmpl}</code> for the text after
          the command and <code>{convTmpl}</code> for prior chat history.
        </p>
        <div className="row" style={{ gap: 8, marginBottom: 8 }}>
          <input
            style={{ flex: 1, minWidth: 180 }}
            placeholder="short name (e.g. code-review)"
            value={newSkill.short_name}
            onChange={(e) => setNewSkill({ ...newSkill, short_name: e.target.value })}
          />
          <input
            style={{ flex: 1, minWidth: 180 }}
            placeholder="display name"
            value={newSkill.name}
            onChange={(e) => setNewSkill({ ...newSkill, name: e.target.value })}
          />
        </div>
        <div className="row" style={{ gap: 8, marginBottom: 8 }}>
          <input
            style={{ flex: 1, minWidth: 180 }}
            placeholder="description"
            value={newSkill.description}
            onChange={(e) => setNewSkill({ ...newSkill, description: e.target.value })}
          />
          <input
            style={{ flex: 1, minWidth: 180 }}
            placeholder="tags (comma-separated)"
            value={newSkill.tags}
            onChange={(e) => setNewSkill({ ...newSkill, tags: e.target.value })}
          />
        </div>
        <textarea
          style={{ width: "100%", minHeight: 120, marginBottom: 8, fontFamily: "monospace", fontSize: 12 }}
          placeholder="prompt template — use {{input}} and {{conversation}}"
          value={newSkill.prompt_template}
          onChange={(e) => setNewSkill({ ...newSkill, prompt_template: e.target.value })}
        />
        <button
          className="btn primary"
          disabled={busy || !newSkill.short_name || !newSkill.prompt_template}
          onClick={create}
        >
          {busy ? "Creating…" : "Create skill"}
        </button>
      </div>

      <div className="card">
        <h2>Installed skills ({skills.length})</h2>
        {skills.length === 0 && <p style={{ opacity: 0.7 }}>No skills created yet.</p>}
        {skills.map((s) => {
          const m = s.manifest;
          const isEditing = editing === s.id;
          return (
            <div
              key={s.id}
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
                    <b>{m.name}</b>
                    <span style={{ opacity: 0.6, fontSize: 12 }}>(/{m.short_name})</span>
                    <span style={{ opacity: 0.6, fontSize: 12 }}>{s.enabled ? "enabled" : "disabled"}</span>
                  </div>
                  {m.description && (
                    <div style={{ opacity: 0.75, fontSize: 13 }}>{m.description}</div>
                  )}
                  {m.tags && m.tags.length > 0 && (
                    <div style={{ opacity: 0.6, fontSize: 12, marginTop: 4 }}>
                      {m.tags.join(", ")}
                    </div>
                  )}
                </div>
                <div className="row" style={{ gap: 4 }}>
                  {!isEditing ? (
                    <>
                      <button className="btn sm" onClick={() => toggleEnabled(s)}>
                        {s.enabled ? "Disable" : "Enable"}
                      </button>
                      <button className="btn sm" onClick={() => setEditing(s.id)}>
                        Edit
                      </button>
                      <button className="btn sm danger" onClick={() => remove(s)}>
                        Remove
                      </button>
                    </>
                  ) : (
                    <>
                      <button className="btn sm primary" onClick={() => update(s.id)}>
                        Save
                      </button>
                      <button className="btn sm" onClick={() => setEditing(null)}>
                        Cancel
                      </button>
                    </>
                  )}
                </div>
              </div>
              {isEditing && (
                <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                  <div className="row" style={{ gap: 8 }}>
                    <input
                      style={{ flex: 1, minWidth: 180 }}
                      placeholder="short name"
                      value={editSkill.short_name}
                      onChange={(e) => setEditSkill({ ...editSkill, short_name: e.target.value })}
                    />
                    <input
                      style={{ flex: 1, minWidth: 180 }}
                      placeholder="display name"
                      value={editSkill.name}
                      onChange={(e) => setEditSkill({ ...editSkill, name: e.target.value })}
                    />
                  </div>
                  <div className="row" style={{ gap: 8 }}>
                    <input
                      style={{ flex: 1, minWidth: 180 }}
                      placeholder="description"
                      value={editSkill.description}
                      onChange={(e) => setEditSkill({ ...editSkill, description: e.target.value })}
                    />
                    <input
                      style={{ flex: 1, minWidth: 180 }}
                      placeholder="tags (comma-separated)"
                      value={editSkill.tags}
                      onChange={(e) => setEditSkill({ ...editSkill, tags: e.target.value })}
                    />
                  </div>
                  <textarea
                    style={{ width: "100%", minHeight: 100, fontFamily: "monospace", fontSize: 12 }}
                    placeholder="prompt template — use {{input}} and {{conversation}}"
                    value={editSkill.prompt_template}
                    onChange={(e) => setEditSkill({ ...editSkill, prompt_template: e.target.value })}
                  />
                </div>
              )}
              {!isEditing && m.prompt_template && (
                <details style={{ marginTop: 4 }}>
                  <summary style={{ fontSize: 12, opacity: 0.7, cursor: "pointer" }}>
                    view template
                  </summary>
                  <pre
                    style={{
                      marginTop: 4,
                      padding: 8,
                      fontSize: 11,
                      fontFamily: "monospace",
                      whiteSpace: "pre-wrap",
                      background: "rgba(127,127,127,0.08)",
                      borderRadius: 4,
                    }}
                  >
                    {m.prompt_template}
                  </pre>
                </details>
              )}
            </div>
          );
        })}
      </div>
    </>
  );
}
