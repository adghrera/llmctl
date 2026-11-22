import { useEffect, useRef, useState } from "react";
import { api, type MCPServer, type MCPToolDef } from "../api/client";
import { ok, err } from "../ui/toast";

export default function MCP() {
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");
  const [args, setArgs] = useState("");
  const [env, setEnv] = useState("");
  const [busy, setBusy] = useState(false);
  const [testResult, setTestResult] = useState<{ id: string; tools: MCPToolDef[] } | null>(null);

  const load = () =>
    api
      .mcpServers()
      .then(setServers)
      .catch((e) => err(e instanceof Error ? e.message : String(e)));

  useEffect(() => {
    load();
  }, []);

  // JSON export/import, same shapes as `llmctl mcp export|import` (F411).
  // NOTE: the file may contain env values that hold secrets.
  const importFileRef = useRef<HTMLInputElement>(null);
  const exportJson = () => {
    const blob = new Blob([JSON.stringify(servers, null, 2) + "\n"], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `llmctl-mcp-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  };
  const importJson = async (f: File) => {
    try {
      const list = JSON.parse(await f.text()) as Array<Record<string, unknown>>;
      if (!Array.isArray(list)) throw new Error("expected a JSON array");
      let created = 0;
      let skipped = 0;
      for (const s of list) {
        const { id: _id, enabled, ...body } = s; // the store mints its own ids
        try {
          const rec = await api.createMCPServer(body as Parameters<typeof api.createMCPServer>[0]);
          if (enabled === false && rec?.id) {
            // create forces enabled=true; re-disable to match the file
            await api.setMCPEnabled(rec.id, false).catch(() => {});
          }
          created++;
        } catch {
          skipped++;
        }
      }
      ok(`imported ${created} MCP servers (${skipped} skipped)`);
      load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const create = async () => {
    if (!name.trim() || !command.trim()) {
      err("name and command are required");
      return;
    }
    setBusy(true);
    try {
      const argList = args.trim() ? args.trim().split(/\s+/) : [];
      const envMap: Record<string, string> = {};
      for (const pair of env.split(",")) {
        const t = pair.trim();
        const i = t.indexOf("=");
        if (i > 0) envMap[t.slice(0, i).trim()] = t.slice(i + 1).trim();
      }
      await api.createMCPServer({
        name: name.trim(),
        command: command.trim(),
        args: argList,
        env: Object.keys(envMap).length ? envMap : undefined,
      });
      ok("MCP server added");
      setName("");
      setCommand("");
      setArgs("");
      setEnv("");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const toggle = async (s: MCPServer) => {
    try {
      await api.setMCPEnabled(s.id, !s.enabled);
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const remove = async (s: MCPServer) => {
    if (!window.confirm(`Remove MCP server "${s.name}"?`)) return;
    try {
      await api.deleteMCPServer(s.id);
      ok("removed");
      await load();
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  const test = async (s: MCPServer) => {
    setBusy(true);
    setTestResult(null);
    try {
      const r = await api.testMCPServer(s.id);
      setTestResult({ id: s.id, tools: r.tools || [] });
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ margin: 0 }}>MCP Servers</h2>
          <div className="row" style={{ gap: 6 }}>
          <button
            className="btn sm"
            disabled={servers.length === 0}
            onClick={() => {
              // Same columns as `llmctl mcp list --csv` (F405).
              const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
              const rows = [
                "id,name,enabled,command",
                ...servers.map((s) =>
                  [s.id, s.name, String(s.enabled), [s.command, ...(s.args ?? [])].join(" ")]
                    .map(esc)
                    .join(",")
                ),
              ];
              const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
              const a = document.createElement("a");
              a.href = URL.createObjectURL(blob);
              a.download = `llmctl-mcp-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.csv`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            Export CSV
          </button>
          <button
            className="btn sm"
            disabled={servers.length === 0}
            title="download servers as JSON — may contain env secrets (CLI: llmctl mcp export)"
            onClick={exportJson}
          >
            Export JSON
          </button>
          <button
            className="btn sm"
            title="import an MCP JSON file (CLI: llmctl mcp import)"
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
        <p className="muted small" style={{ margin: "4px 0 12px" }}>
          Model Context Protocol servers launched as local processes. Their
          tools are offered to the agent as <span className="mono">mcp:&lt;id&gt;:&lt;tool&gt;</span>.
        </p>
        <div className="row" style={{ gap: 8, marginBottom: 12, flexWrap: "wrap" }}>
          <input placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} />
          <input
            placeholder="Command (e.g. npx)"
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            style={{ minWidth: 160 }}
          />
          <input
            placeholder="Arguments (space-separated)"
            value={args}
            onChange={(e) => setArgs(e.target.value)}
            style={{ flex: 1, minWidth: 200 }}
          />
          <input
            placeholder="Env (K=V, K2=V2)"
            title="server process environment (CLI: llmctl mcp add -env)"
            value={env}
            onChange={(e) => setEnv(e.target.value)}
            style={{ flex: 1, minWidth: 160 }}
          />
          <button className="btn primary" disabled={busy} onClick={create}>
            {busy ? "Working…" : "Add server"}
          </button>
        </div>
        {servers.length === 0 ? (
          <div className="empty">no MCP servers configured</div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Command</th>
                <th>Enabled</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {servers.map((s) => (
                <tr key={s.id}>
                  <td>
                    {s.name}
                    <div className="muted" style={{ fontSize: 11 }}>{s.id}</div>
                  </td>
                  <td className="mono" style={{ fontSize: 12 }}>
                    {[s.command, ...(s.args || [])].join(" ")}
                  </td>
                  <td style={{ textAlign: "center" }}>
                    <input type="checkbox" checked={s.enabled} onChange={() => toggle(s)} />
                  </td>
                  <td>
                    <div className="row" style={{ gap: 6 }}>
                      <button className="btn sm" disabled={busy} onClick={() => test(s)}>
                        Test
                      </button>
                      <button className="btn sm danger" onClick={() => remove(s)}>
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      {testResult && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Tools — {testResult.id}</h2>
            <button
              className="btn sm"
              onClick={() => {
                // Same columns as `llmctl mcp test --csv` (F437).
                const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
                const rows = [
                  "name,description",
                  ...testResult.tools.map((t) => [t.name, t.description || ""].map(esc).join(",")),
                ];
                const blob = new Blob([rows.join("\n") + "\n"], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-mcp-tools-${testResult.id}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
            <button
              className="btn sm"
              title="download the advertised tools as JSON"
              onClick={() => {
                const blob = new Blob([JSON.stringify(testResult.tools, null, 2)], { type: "application/json" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-mcp-tools-${testResult.id}.json`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export JSON
            </button>
          </div>
          {testResult.tools.length === 0 ? (
            <div className="empty">server connected but advertises no tools</div>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>Tool</th>
                  <th>Description</th>
                </tr>
              </thead>
              <tbody>
                {testResult.tools.map((t) => (
                  <tr key={t.name}>
                    <td className="mono">{t.name}</td>
                    <td>{t.description || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </>
  );
}
