import { useStore } from "../state/store";

export default function Overview() {
  const { status, instances, jobs } = useStore();
  const running = instances.filter((i) => i.status === "running");
  const activeJobs = jobs.filter((j) => j.status === "running");
  const completedJobs = jobs
    .filter(j => j.status === "done" || j.status === "error")
    .sort((a, b) => new Date(b.created).getTime() - new Date(a.created).getTime())
    .slice(0, 5);

  return (
    <>
      <div className="card">
        <h2>Status</h2>
        {status ? (
          <p>
            llmctl <span className="mono">{status.version}</span> —{" "}
            {status.backends} backends, {status.models} models,{" "}
            {status.instances} instances.
          </p>
        ) : (
          <div className="empty">connecting to daemon…</div>
        )}
      </div>
      <div className="card">
        <h2>Running instances</h2>
        {running.length ? (
          <ul>
            {running.map((i) => (
              <li key={i.id} className="mono">
                {i.id} <span className="muted">({i.backendId}/{i.transport})</span>
              </li>
            ))}
          </ul>
        ) : (
          <div className="empty">no instances running</div>
        )}
      </div>
      {activeJobs.length > 0 && (
        <div className="card">
          <h2>Active jobs</h2>
          <ul>
            {activeJobs.map((j) => (
              <li key={j.id} className="mono">
                {j.kind} {j.target} — {Math.round(j.progress * 100)}%
              </li>
            ))}
          </ul>
        </div>
      )}
      {completedJobs.length > 0 && (
        <div className="card">
          <h2>Recent jobs</h2>
          <ul>
            {completedJobs.map((j) => (
              <li key={j.id} className="mono">
                {j.kind} {j.target} — {Math.round(j.progress * 100)}%{j.status === "error" && ` (error: ${j.error})`}
              </li>
            ))}
          </ul>
        </div>
      )}
    </>
  );
}
