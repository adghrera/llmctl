import { useEffect, useState } from "react";
import { api, type GatewayMetrics } from "../api/client";
import { err } from "../ui/toast";

export default function Usage() {
  const [metrics, setMetrics] = useState<GatewayMetrics | null>(null);
  const [chats, setChats] = useState<any[]>([]);

  const load = async () => {
    try {
      const [m, c] = await Promise.all([
        api.metrics().catch(() => null as GatewayMetrics | null),
        api.chats().catch(() => [] as any[]),
      ]);
      setMetrics(m);
      setChats(c);
    } catch (e) {
      err(e instanceof Error ? e.message : String(e));
    }
  };

  useEffect(() => {
    load();
    const t = setInterval(load, 10000);
    return () => clearInterval(t);
  }, []);

  if (!metrics) {
    return <div className="card">Loading usage data…</div>;
  }

  const models = Object.entries(metrics.models || {}).map(
    ([id, v]: [string, any]) => ({
      id,
      requests: v.requests || 0,
      errors: v.errors || 0,
      prompt: v.prompt_tokens || 0,
      completion: v.completion_tokens || 0,
      total: (v.prompt_tokens || 0) + (v.completion_tokens || 0),
    })
  );

  const totalPrompt = metrics.tokens?.prompt || 0;
  const totalCompletion = metrics.tokens?.completion || 0;
  const totalTokens = totalPrompt + totalCompletion;

  // Max bar width for normalisation.
  const maxModelTokens =
    models.length > 0
      ? Math.max(...models.map((m) => m.total))
      : 0;

  return (
    <>
      <div className="card">
        <h2>Token Usage</h2>
        <p style={{ opacity: 0.75, margin: "4px 0 14px" }}>
          Token usage is parsed from OpenAI "usage" fields in gateway
          responses (~4 chars/token estimate when usage is absent). Counters
          reset when the daemon restarts; per-conversation totals are
          persisted in chat history.
        </p>
        <div className="row" style={{ gap: 16, marginBottom: 16 }}>
          <div style={{ flex: 1, minWidth: 200 }}>
            <div style={{ fontSize: 12, opacity: 0.7 }}>Total prompt tokens</div>
            <div style={{ fontSize: 24, fontFamily: "monospace" }}>
              {totalPrompt.toLocaleString()}
            </div>
          </div>
          <div style={{ flex: 1, minWidth: 200 }}>
            <div style={{ fontSize: 12, opacity: 0.7 }}>Total completion tokens</div>
            <div style={{ fontSize: 24, fontFamily: "monospace" }}>
              {totalCompletion.toLocaleString()}
            </div>
          </div>
          <div style={{ flex: 1, minWidth: 200 }}>
            <div style={{ fontSize: 12, opacity: 0.7 }}>Grand total</div>
            <div style={{ fontSize: 24, fontFamily: "monospace" }}>
              {totalTokens.toLocaleString()}
            </div>
          </div>
          <div style={{ flex: 1, minWidth: 200 }}>
            <div style={{ fontSize: 12, opacity: 0.7 }}>Requests</div>
            <div style={{ fontSize: 24, fontFamily: "monospace" }}>
              {metrics.requests_total.toLocaleString()}
            </div>
          </div>
        </div>
      </div>

      {models.length > 0 && (
        <div className="card">
          <h2>Usage by Model</h2>
          <table>
            <thead>
              <tr>
                <th>Model</th>
                <th>Requests</th>
                <th>Errors</th>
                <th>Prompt</th>
                <th>Completion</th>
                <th>Total</th>
                <th>Share</th>
              </tr>
            </thead>
            <tbody>
              {models.map((m) => {
                const pct = maxModelTokens > 0 ? (m.total / maxModelTokens) * 100 : 0;
                return (
                  <tr key={m.id}>
                    <td style={{ fontFamily: "monospace", fontSize: 12 }}>{m.id}</td>
                    <td className="oc-num">{m.requests}</td>
                    <td className="oc-num">{m.errors}</td>
                    <td className="oc-num">{m.prompt.toLocaleString()}</td>
                    <td className="oc-num">{m.completion.toLocaleString()}</td>
                    <td className="oc-num">{m.total.toLocaleString()}</td>
                    <td>
                      <div
                        style={{
                          height: 8,
                          background: "rgba(127,127,127,0.2)",
                          borderRadius: 4,
                          overflow: "hidden",
                          width: "100%",
                        }}
                      >
                        <div
                          style={{
                            height: "100%",
                            width: `${pct}%`,
                            background: pct > 70 ? "#c90" : "#3a6",
                            borderRadius: 4,
                          }}
                        />
                      </div>
                      <span style={{ fontSize: 11, opacity: 0.7 }}>
                        {pct.toFixed(0)}%
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {chats.length > 0 && (
        <div className="card">
          <h2>Usage by Conversation</h2>
          <p style={{ fontSize: 12, opacity: 0.7, marginBottom: 8 }}>
            Estimated tokens (~4 chars/token) across all messages in each
            saved conversation.
          </p>
          <table>
            <thead>
              <tr>
                <th>Conversation</th>
                <th>Model</th>
                <th>Msgs</th>
                <th>Tokens</th>
                <th>Ctx %</th>
              </tr>
            </thead>
            <tbody>
              {chats.map((c) => {
                const used = c.context?.usedTokens || 0;
                const limit = c.context?.limitTokens || 0;
                const pct = c.context?.pct || 0;
                return (
                  <tr key={c.id}>
                    <td>{c.title}</td>
                    <td style={{ fontFamily: "monospace", fontSize: 12 }}>
                      {c.model || "—"}
                    </td>
                    <td className="oc-num">{c.messages}</td>
                    <td className="oc-num">{used.toLocaleString()}</td>
                    <td>
                      <div
                        style={{
                          height: 8,
                          background: "rgba(127,127,127,0.2)",
                          borderRadius: 4,
                          overflow: "hidden",
                          width: "100%",
                        }}
                      >
                        <div
                          style={{
                            height: "100%",
                            width: `${Math.min(pct, 100)}%`,
                            background:
                              pct >= 98 ? "#c44" : pct >= 80 ? "#c90" : "#3a6",
                            borderRadius: 4,
                          }}
                        />
                      </div>
                      <span style={{ fontSize: 11, opacity: 0.7 }}>
                        {limit > 0 ? pct.toFixed(0) + "%" : "—"}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {!models.length && !chats.length && (
        <div className="card">
          <p style={{ opacity: 0.7 }}>No usage data yet. Start a conversation to generate tokens.</p>
        </div>
      )}
    </>
  );
}
