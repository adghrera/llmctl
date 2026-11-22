import { useEffect, useState } from "react";
import { api, type GatewayMetrics, type ApiKey } from "../api/client";
import { err } from "../ui/toast";

function csvField(s: string): string {
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

type DailyUsage = NonNullable<Awaited<ReturnType<typeof api.dailyUsage>>>;

// trendDays sums per-day totals from the daily rollup, oldest first, capped
// at the newest 14 days (F147).
export function trendDays(daily: DailyUsage) {
  return Object.entries(daily)
    .sort((a, b) => a[0].localeCompare(b[0]))
    .slice(-14)
    .map(([date, models]) => {
      let requests = 0;
      let cost = 0;
      for (const u of Object.values(models)) {
        requests += u.requests;
        cost += u.cost ?? 0;
      }
      return { date, requests, cost };
    });
}

// TrendChart draws the last 14 UTC days as two small bar charts (requests and
// estimated cost) from the daily rollup (F147).
function TrendChart({ daily }: { daily: DailyUsage }) {
  const days = trendDays(daily);
  if (days.length < 2) return null;
  const W = 300;
  const H = 90;
  const bw = W / days.length;
  const bars = (values: number[], color: string, fmt: (v: number) => string) => {
    const max = Math.max(...values, 1);
    return (
      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: "100%", maxWidth: 420, height: "auto" }}>
        {values.map((v, i) => {
          const h = Math.max(1, (v / max) * (H - 16));
          return (
            <rect
              key={i}
              x={i * bw + bw * 0.15}
              y={H - 14 - h}
              width={bw * 0.7}
              height={h}
              fill={color}
              rx={1}
            >
              <title>{`${days[i].date}: ${fmt(v)}`}</title>
            </rect>
          );
        })}
        {days.map((d, i) =>
          i % Math.ceil(days.length / 7) === 0 ? (
            <text key={i} x={i * bw + bw / 2} y={H - 3} fontSize={7} fill="var(--muted)" textAnchor="middle">
              {d.date.slice(5)}
            </text>
          ) : null,
        )}
      </svg>
    );
  };
  return (
    <div className="card">
      <h2>Trend (last {days.length} days, UTC)</h2>
      <div style={{ display: "flex", gap: 24, flexWrap: "wrap" }}>
        <div style={{ flex: "1 1 260px" }}>
          <div className="muted small" style={{ marginBottom: 4 }}>
            requests/day — peak {Math.max(...days.map((d) => d.requests))}
          </div>
          {bars(days.map((d) => d.requests), "var(--accent)", (v) => `${v} requests`)}
        </div>
        <div style={{ flex: "1 1 260px" }}>
          <div className="muted small" style={{ marginBottom: 4 }}>
            cost/day — peak ${Math.max(...days.map((d) => d.cost)).toFixed(4)}
          </div>
          {bars(days.map((d) => d.cost), "var(--accent2)", (v) => `$${v.toFixed(4)}`)}
        </div>
      </div>
    </div>
  );
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

export function dailyKeyToCsv(daily: NonNullable<Awaited<ReturnType<typeof api.dailyKeyUsage>>>): string {
  const head = ["date", "key_id", "requests", "errors", "prompt_tokens", "completion_tokens", "cost_usd"];
  const rows: string[] = [];
  for (const [date, keys] of Object.entries(daily).sort((a, b) => b[0].localeCompare(a[0]))) {
    for (const [key, u] of Object.entries(keys)) {
      rows.push(
        [date, key, String(u.requests), String(u.errors), String(u.prompt), String(u.comp), (u.cost ?? 0).toFixed(6)]
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
  const [dailyKeys, setDailyKeys] = useState<Awaited<ReturnType<typeof api.dailyKeyUsage>> | null>(null);
  const [keys, setKeys] = useState<ApiKey[]>([]);

  const load = async () => {
    try {
      const [m, c, d, dk, ks] = await Promise.all([
        api.metrics().catch(() => null as GatewayMetrics | null),
        api.chats().catch(() => [] as any[]),
        api.dailyUsage().catch(() => null),
        api.dailyKeyUsage().catch(() => null),
        api.keys().catch(() => [] as ApiKey[]),
      ]);
      setMetrics(m);
      setChats(c);
      setDaily(d);
      setDailyKeys(dk);
      setKeys(ks);
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
      err4xx: v.errors_4xx || 0,
      err5xx: v.errors_5xx || 0,
      prompt: v.prompt_tokens || 0,
      completion: v.completion_tokens || 0,
      total: (v.prompt_tokens || 0) + (v.completion_tokens || 0),
      p50: v.latency_p50_ms,
      p95: v.latency_p95_ms,
      p99: v.latency_p99_ms,
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
                <th>p99</th>
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
                    <td className="oc-num" title={m.errors > 0 ? `${m.err4xx} client · ${m.err5xx} server` : undefined}>
                      {m.errors}
                      {m.errors > 0 && (
                        <span className="muted" style={{ fontSize: 11 }}>
                          {" "}({m.err4xx}c/{m.err5xx}s)
                        </span>
                      )}
                    </td>
                    <td className="oc-num">{m.p50 != null ? m.p50.toFixed(0) + " ms" : "—"}</td>
                    <td className="oc-num">{m.p95 != null ? m.p95.toFixed(0) + " ms" : "—"}</td>
                    <td className="oc-num">{m.p99 != null ? m.p99.toFixed(0) + " ms" : "—"}</td>
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

      {daily && Object.keys(daily).length >= 2 && <TrendChart daily={daily} />}

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

      {dailyKeys && Object.keys(dailyKeys).length > 0 && (
        <div className="card">
          <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h2 style={{ margin: 0 }}>Daily by Key</h2>
            <button
              className="btn sm"
              onClick={() => {
                const blob = new Blob([dailyKeyToCsv(dailyKeys)], { type: "text/csv" });
                const a = document.createElement("a");
                a.href = URL.createObjectURL(blob);
                a.download = `llmctl-daily-key-usage-${new Date().toISOString().slice(0, 10)}.csv`;
                a.click();
                URL.revokeObjectURL(a.href);
              }}
            >
              Export CSV
            </button>
          </div>
          <p style={{ fontSize: 12, opacity: 0.7, marginBottom: 8 }}>
            Gateway usage per UTC day attributed to each API key.
          </p>
          <div className="oc-table">
            <table>
              <thead>
                <tr>
                  <th>Date</th>
                  <th>Key</th>
                  <th>Requests</th>
                  <th>Errors</th>
                  <th>Prompt</th>
                  <th>Completion</th>
                  <th>Cost</th>
                </tr>
              </thead>
              <tbody>
                {Object.entries(dailyKeys)
                  .sort((a, b) => b[0].localeCompare(a[0]))
                  .flatMap(([date, keysOnDay]) =>
                    Object.entries(keysOnDay)
                      .sort((a, b) => b[1].requests - a[1].requests)
                      .map(([keyId, u]) => {
                        const k = keys.find((x) => x.id === keyId);
                        return (
                          <tr key={date + keyId}>
                            <td style={{ fontFamily: "monospace", fontSize: 12 }}>{date}</td>
                            <td>
                              {k ? k.name : <span className="muted">(deleted)</span>}{" "}
                              <span className="muted mono" style={{ fontSize: 11 }}>{keyId.slice(0, 8)}</span>
                            </td>
                            <td className="oc-num">{u.requests.toLocaleString()}</td>
                            <td className="oc-num">{u.errors}</td>
                            <td className="oc-num">{u.prompt.toLocaleString()}</td>
                            <td className="oc-num">{u.comp.toLocaleString()}</td>
                            <td className="oc-num">{u.cost ? `$${u.cost.toFixed(4)}` : "—"}</td>
                          </tr>
                        );
                      }),
                  )}
              </tbody>
            </table>
          </div>
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
