import { useEffect, useState } from "react";
import { api, ToolRecord, ToolRunResult } from "../api/client";
import { ok, err } from "../ui/toast";

export default function Tools() {
  const [tools, setTools] = useState<ToolRecord[]>([]);
  const [url, setUrl] = useState("");
  const [dir, setDir] = useState("");
  const [busy, setBusy] = useState(false);
  const [running, setRunning] = useState<string | null>(null);
  const [runResult, setRunResult] = useState<Record<string, ToolRunResult>>({});
  const [runArgs, setRunArgs] = useState<Record<string, string>>({});

  const load = () =>
    api
      .tools()
      .then(setTools)
      .catch((e) => err(e instanceof Error ? e.message : String(e)));

  useEffect(() => {
    load();
  }, []);

  const install = async (body: { url?: string; dir?: string }) => {
    setBusy(true);
    try {
      const rec = await api.installTool(body);
      ok(`installed ${rec.manifest.name}`);
      setUrl("");
      setDir("");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const toggle = async (t: ToolRecord, field: "enabled" | "sandbox") => {
    const next = field === "enabled" ? !t.enabled : !t.sandbox;
    try {
      if (field === "enabled") await api.setToolEnabled(t.id, next);
      else await api.setToolSandbox(t.id, next);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const uninstall = async (t: ToolRecord) => {
    if (!window.confirm(`Uninstall tool "${t.manifest.name}"? Its files are removed.`)) return;
    try {
      await api.deleteTool(t.id);
      ok(`uninstalled ${t.manifest.name}`);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const testRun = async (t: ToolRecord) => {
    setRunning(t.id);
    setRunResult((r) => ({ ...r, [t.id]: undefined as unknown as ToolRunResult }));
    let args: Record<string, unknown> = {};
    const raw = runArgs[t.id] ?? "";
    if (raw.trim()) {
      try {
        args = JSON.parse(raw);
      } catch {
        err("args must be JSON, e.g. {\"text\": \"hello world\"}");
        setRunning(null);
        return;
      }
    }
    try {
      const res = await api.runTool(t.id, args, 120);
      setRunResult((r) => ({ ...r, [t.id]: res }));
      res.ok ? ok(`ran in ${res.duration}`) : err(res.error || "tool failed");
    } catch (e) {
      setRunResult((r) => ({
        ...r,
        [t.id]: { ok: false, error: e instanceof Error ? e.message : String(e), exitCode: -1, sandbox: t.sandbox, duration: "" },
      }));
    } finally {
      setRunning(null);
    }
  };

  return (
    <>
      <div className="card">
        <h2>Install a tool</h2>
        <p style={{ opacity: 0.75, margin: "4px 0 10px" }}>
          A tool package is a directory (or .zip) containing <code>llmctl-tool.json</code> and a{" "}
          <code>def run(args) -&gt; str</code> Python entry. Tools run <b>inside the llm-sandbox</b> by
          default (container, no network, memory &amp; PID caps); you can allow host execution per tool.
        </p>
        <div className="row">
          <input
            style={{ flex: 1, minWidth: 240 }}
            placeholder="Package URL (https://…/tool.zip)"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && url && install({ url })}
          />
          <button className="btn primary" disabled={busy || !url} onClick={() => install({ url })}>
            Install from URL
          </button>
        </div>
        <div className="row" style={{ marginTop: 8 }}>
          <input
            style={{ flex: 1, minWidth: 240 }}
            placeholder="Local directory (C:\\path\\to\\tool)"
            value={dir}
            onChange={(e) => setDir(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && dir && install({ dir })}
          />
          <button className="btn" disabled={busy || !dir} onClick={() => install({ dir })}>
            Install from folder
          </button>
        </div>
      </div>

      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>Installed tools ({tools.length})</h2>
          <button
            className="btn sm"
            disabled={tools.length === 0}
            onClick={() => {
              // Same columns as `llmctl tools list --csv` (F396).
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "id,enabled,sandbox,runtime,name,description",
                ...tools.map((t) =>
                  [
                    t.id, String(t.enabled), String(t.sandbox), t.manifest.runtime || "python",
                    t.manifest.name, t.manifest.description || "",
                  ]
                    .map(esc)
                    .join(",")
                ),
              ];
              const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-tools-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export CSV
          </button>
        </div>
        {tools.length === 0 && <p style={{ opacity: 0.7 }}>No tools installed yet.</p>}
        {tools.map((t) => {
          const res = runResult[t.id];
          return (
            <div key={t.id} style={{ borderTop: "1px solid var(--border, #333)", padding: "12px 0" }}>
              <div className="row" style={{ justifyContent: "space-between" }}>
                <div>
                  <b>{t.manifest.name}</b>{" "}
                  <span style={{ opacity: 0.6, fontSize: 12 }}>({t.id})</span>
                  <div style={{ opacity: 0.75, fontSize: 13 }}>{t.manifest.description}</div>
                  <div style={{ opacity: 0.55, fontSize: 12, marginTop: 2 }}>
                    runtime: {t.manifest.runtime || "python"} · entry: {t.manifest.entry} · from{" "}
                    {t.source || "local"}
                    {t.sandbox && (
                      <>
                        {" · "}
                        <span style={{ color: t.network ? "#c90" : "#3a6" }}>
                          {t.network ? "sandbox + network" : "sandbox, no network"}
                        </span>
                      </>
                    )}
                  </div>
                </div>
                <div className="row">
                  <label style={{ fontSize: 13, marginRight: 8 }}>
                    <input type="checkbox" checked={t.enabled} onChange={() => toggle(t, "enabled")} /> enabled
                  </label>
                  <label style={{ fontSize: 13, marginRight: 8, opacity: t.manifest.runtime === "command" ? 0.4 : 1 }}>
                    <input
                      type="checkbox"
                      checked={t.sandbox}
                      disabled={t.manifest.runtime === "command"}
                      onChange={() => toggle(t, "sandbox")}
                    />{" "}
                    sandbox
                  </label>
                  <button className="btn" disabled={running === t.id} onClick={() => testRun(t)}>
                    {running === t.id ? "Running…" : "Test run"}
                  </button>
                  <button className="btn danger" onClick={() => uninstall(t)}>
                    Uninstall
                  </button>
                </div>
              </div>
              <details style={{ marginTop: 6 }}>
                <summary style={{ fontSize: 12, opacity: 0.7, cursor: "pointer" }}>test-run args (JSON)</summary>
                <textarea
                  style={{ width: "100%", minHeight: 48, marginTop: 4, fontFamily: "monospace", fontSize: 12 }}
                  placeholder={'{"text": "hello world"}'}
                  value={runArgs[t.id] ?? ""}
                  onChange={(e) => setRunArgs((m) => ({ ...m, [t.id]: e.target.value }))}
                />
              </details>
              {res !== undefined && (
                <pre
                  style={{
                    marginTop: 6,
                    padding: 8,
                    fontSize: 12,
                    whiteSpace: "pre-wrap",
                    borderLeft: `3px solid ${res.ok ? "#3a6" : "#c44"}`,
                    background: "rgba(127,127,127,0.08)",
                  }}
                >
                  {res.ok
                    ? `${res.sandbox ? `[sandbox${res.backend ? ":" + res.backend : ""}]` : "[host]"} ${res.duration}\n${res.result}`
                    : `FAILED: ${res.error || "exit " + res.exitCode}${res.stderr ? "\n" + res.stderr : ""}`}
                </pre>
              )}
            </div>
          );
        })}
      </div>
    </>
  );
}
