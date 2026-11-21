import { useEffect, useState } from "react";
import { api, type MCPServer, type MCPToolDef } from "../api/client";
import { ok, err } from "../ui/toast";

export default function MCP() {
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");
  const [args, setArgs] = useState("");
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

  const create = async () => {
    if (!name.trim() || !command.trim()) {
      err("name and command are required");
      return;
    }
    setBusy(true);
    try {
      const argList = args.trim() ? args.trim().split(/\s+/) : [];
      await api.createMCPServer({ name: name.trim(), command: command.trim(), args: argList });
      ok("MCP server added");
      setName("");
      setCommand("");
      setArgs("");
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
        <h2>MCP Servers</h2>
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
          <h2>Tools — {testResult.id}</h2>
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
