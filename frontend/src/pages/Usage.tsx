import { useEffect, useState } from "react";
import { api, type GatewayMetrics } from "../api/client";
import { err } from "../ui/toast";

function csvField(s: string): string {
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

export function dailyToCsv(daily: NonNullable<Awaited<ReturnType<typeof api.dailyUsage>>>): string {
  const head = ["date", "model", "requests", "errors", "prompt_tokens", "completion_tokens", "cost_usd"];
  const rows: string[] = [];
  for (const [date, models] of Object.entries(daily).sort((a, b) => b[0].localeCompare(a[0]))) {
    for (const [model, u] of Object.entries(models)) {
      rows.push(
        [date, model, String(u.requests), String(u.errors), String(u.prompt), String(u.comp), (u.cost ?? 0).toFixed(6)]
          .map(csvField)
          .join(","),
      );
    }
  }
  return [head.join(","), ...rows].join("\n") + "\n";
}

export default function Usage() {
  const [metrics, setMetrics] = useState<GatewayMetrics | null>(null);
  const [chats, setChats] = useState<any[]>([]);
  const [daily, setDaily] = useState<Awaited<ReturnType<typeof api.dailyUsage>> | null>(null);

  const load = async () => {
    try {
      const [m, c, d] = await Promise.all([
        api.metrics().catch(() => null as GatewayMetrics | null),
        api.chats().catch(() => [] as any[]),
        api.dailyUsage().catch(() => null),
      ]);
      setMetrics(m);
      setChats(c);
      setDaily(d);
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
      p50: v.latency_p50_ms,
      p95: v.latency_p95_ms,
      tps: v.tokens_per_sec,
      ttft: v.ttft_avg_ms,
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
                <th>p50</th>
                <th>p95</th>
                <th>ttft</th>
                <th>tok/s</th>
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
                    <td className="oc-num">{m.p50 != null ? m.p50.toFixed(0) + " ms" : "—"}</td>
                    <td className="oc-num">{m.p95 != null ? m.p95.toFixed(0) + " ms" : "—"}</td>
                    <td className="oc-num">{m.ttft != null ? m.ttft.toFixed(0) + " ms" : "—"}</td>
                    <td className="oc-num">{m.tps != null ? m.tps.toFixed(1) : "—"}</td>
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

      {daily && Object.keys(daily).length > 0 && (
        <div className="card">
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Daily History</h2>
            <button
              className="btn sm"
              onClick={() => {
                const blob = new Blob([dailyToCsv(daily)], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-daily-usage-${new Date().toISOString().slice(0, 10)}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
          </div>
          <p style={{ fontSize: 12, opacity: 0.7, marginBottom: 8 }}>
            Gateway usage per UTC day (last {Object.keys(daily).length} days
            with traffic); survives daemon restarts.
          </p>
          <table>
            <thead>
              <tr>
                <th>Date</th>
                <th>Requests</th>
                <th>Errors</th>
                <th>Prompt</th>
                <th>Completion</th>
                <th>Total</th>
                <th>Cost</th>
              </tr>
            </thead>
            <tbody>
              {Object.entries(daily)
                .sort((a, b) => b[0].localeCompare(a[0]))
                .map(([date, models]) => {
                  let reqs = 0, errs = 0, prompt = 0, comp = 0, cost = 0;
                  for (const u of Object.values(models)) {
                    reqs += u.requests;
                    errs += u.errors;
                    prompt += u.prompt;
                    comp += u.comp;
                    cost += u.cost ?? 0;
                  }
                  return (
                    <tr key={date}>
                      <td style={{ fontFamily: "monospace", fontSize: 12 }}>{date}</td>
                      <td className="oc-num">{reqs.toLocaleString()}</td>
                      <td className="oc-num">{errs}</td>
                      <td className="oc-num">{prompt.toLocaleString()}</td>
                      <td className="oc-num">{comp.toLocaleString()}</td>
                      <td className="oc-num">{(prompt + comp).toLocaleString()}</td>
                      <td className="oc-num">{cost > 0 ? `$${cost.toFixed(4)}` : "—"}</td>
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
