import { useEffect, useRef, useState } from "react";
import { api, chatStream, SkillRecord } from "../api/client";
import { ok, err } from "../ui/toast";

export default function Skills() {
  const inputTmpl = "{{input}}";
  const convTmpl = "{{conversation}}";
  const [skills, setSkills] = useState<SkillRecord[]>([]);
  const [tagFilter, setTagFilter] = useState(""); // tag filter, CLI `skills -tag` twin (F707)
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
  // Run-a-skill panel: render {{input}} and stream a reply (Prompts Run twin, F833).
  const [runFor, setRunFor] = useState<string | null>(null);
  const [runInput, setRunInput] = useState("");
  const [runModel, setRunModel] = useState("");
  const [modelChoices, setModelChoices] = useState<string[]>([]);
  const [runReply, setRunReply] = useState<{ id: string; prompt: string; text: string; busy: boolean } | null>(null);

  const runSkill = async (s: SkillRecord) => {
    if (!runModel) {
      err("pick a model first");
      return;
    }
    const rendered = (s.manifest.prompt_template || "")
      .split("{{input}}")
      .join(runInput)
      .split("{{conversation}}")
      .join("(none)");
    setRunReply({ id: s.id, prompt: rendered, text: "", busy: true });
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

  const skillTags = Array.from(new Set(skills.flatMap((s) => s.manifest.tags ?? []))).sort();
  const shownSkills = tagFilter
    ? skills.filter((s) => (s.manifest.tags ?? []).includes(tagFilter))
    : skills;

  const load = () =>
    api
      .skills()
      .then(setSkills)
      .catch((e) => err(e instanceof Error ? e.message : String(e)));

  useEffect(() => {
    load();
  }, []);

  // JSON export/import, same shapes as `llmctl skills export` + `skills add` (F415).
  const importFileRef = useRef<HTMLInputElement>(null);
  const exportSkill = (s: SkillRecord) => {
    const blob = new Blob([JSON.stringify(s.manifest, null, 2) + "\n"], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `llmctl-skill-${s.manifest.short_name || s.id}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  };
  const importSkill = async (f: File) => {
    try {
      const m = JSON.parse(await f.text()) as Record<string, unknown>;
      if (!m.short_name || !m.prompt_template) throw new Error("manifest needs short_name and prompt_template");
      await api.createSkill({
        short_name: String(m.short_name),
        name: m.name ? String(m.name) : undefined,
        description: m.description ? String(m.description) : undefined,
        prompt_template: String(m.prompt_template),
        tags: Array.isArray(m.tags) ? (m.tags as string[]) : undefined,
      });
      ok(`imported skill /s.${m.short_name}`);
      load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

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
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Installed skills ({skills.length})</h2>
          <div className="row" style={{ gap: 6 }}>
          <button
            className="btn sm"
            disabled={skills.length === 0}
            onClick={() => {
              // Same columns as `llmctl skills list --csv` (F395).
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "id,enabled,name,description,tags",
                ...skills.map((s) =>
                  [
                    s.manifest.id, s.manifest.enabled ? "on" : "off", s.manifest.name,
                    s.manifest.description || "", (s.manifest.tags ?? []).join(";"),
                  ]
                    .map(esc)
                    .join(",")
                ),
              ];
              const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-skills-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export CSV
          </button>
          <button
            className="btn sm"
            disabled={skills.length === 0}
            title="download the skills list as JSON (CLI: llmctl skills list -json)"
            onClick={() => {
              const blob = new Blob([JSON.stringify(skills, null, 2)], { type: "application/json" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-skills-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export JSON
          </button>
          <button
            className="btn sm"
            title="import a skill manifest JSON (CLI: llmctl skills export → skills add)"
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
              if (f) importSkill(f);
              e.target.value = "";
            }}
          />
          {skillTags.length > 0 && (
            <select
              className="oc-input"
              style={{ width: "auto", fontSize: 12 }}
              title="filter by tag — CLI: llmctl skills -tag (F707)"
              value={tagFilter}
              onChange={(e) => setTagFilter(e.target.value)}
            >
              <option value="">all tags</option>
              {skillTags.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          )}
          </div>
        </div>
        {skills.length === 0 && <p style={{ opacity: 0.7 }}>No skills created yet.</p>}
        {shownSkills.map((s) => {
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
                      <button
                        className="btn sm"
                        title="render and run this skill against a model (Prompts Run twin, F833)"
                        onClick={() => {
                          if (runFor === s.id) {
                            setRunFor(null);
                            return;
                          }
                          setRunFor(s.id);
                          if (modelChoices.length === 0) {
                            api.models().then((ms) => setModelChoices(ms.map((m) => m.id))).catch(() => {});
                          }
                        }}
                      >
                        {runFor === s.id ? "Hide run" : "Run"}
                      </button>
                      <button
                        className="btn sm"
                        title="copy the skill's prompt template to the clipboard (F784)"
                        onClick={() => {
                          navigator.clipboard
                            ?.writeText(m.prompt_template || "")
                            .then(() => ok("copied"))
                            .catch(() => {});
                        }}
                      >
                        Copy prompt
                      </button>
                      <button
                        className="btn sm"
                        title="download the manifest JSON (CLI: llmctl skills export)"
                        onClick={() => exportSkill(s)}
                      >
                        Export
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
              {runFor === s.id && !isEditing && (
                <div style={{ display: "flex", flexDirection: "column", gap: 6, marginTop: 6 }}>
                  <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
                    <input
                      style={{ flex: 1, minWidth: 180 }}
                      placeholder="{{input}} for the template"
                      value={runInput}
                      onChange={(e) => setRunInput(e.target.value)}
                    />
                    <select
                      style={{ width: 180 }}
                      title="model to send the rendered skill to"
                      value={runModel}
                      onChange={(e) => setRunModel(e.target.value)}
                    >
                      <option value="">model…</option>
                      {modelChoices.map((m) => (
                        <option key={m} value={m}>
                          {m}
                        </option>
                      ))}
                    </select>
                    <button
                      className="btn sm primary"
                      disabled={runReply?.busy || !runModel}
                      onClick={() => runSkill(s)}
                    >
                      {runReply?.busy && runReply.id === s.id ? "Running…" : "Run"}
                    </button>
                  </div>
                  {runReply && runReply.id === s.id && (
                    <div style={{ position: "relative" }}>
                      <pre
                        style={{
                          margin: 0,
                          fontSize: 12,
                          whiteSpace: "pre-wrap",
                          background: "var(--code-background, rgba(127,127,127,0.08))",
                          padding: 8,
                          borderRadius: 4,
                        }}
                      >
                        {runReply.text}
                      </pre>
                      {runReply.text && (
                        <button
                          className="btn xs copy"
                          style={{ position: "absolute", top: 4, right: 4 }}
                          title="copy the reply"
                          onClick={() =>
                            navigator.clipboard?.writeText(runReply.text).then(() => ok("copied")).catch(() => {})
                          }
                        >
                          ⧉
                        </button>
                      )}
                    </div>
                  )}
                  {runReply && runReply.id === s.id && !runReply.busy && runReply.text && (
                    <button
                      className="btn sm"
                      style={{ alignSelf: "flex-start" }}
                      title="save the rendered skill and reply as a saved chat (CLI: skills run -save, F839)"
                      onClick={async () => {
                        try {
                          const c = await api.createChat({
                            title: s.manifest.name || s.manifest.short_name,
                            model: runModel,
                            messages: [
                              { role: "user", content: runReply.prompt },
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
                </div>
              )}
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
