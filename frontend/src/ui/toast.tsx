import { useEffect, useState } from "react";

type Kind = "info" | "ok" | "err";
interface Toast {
  id: number;
  kind: Kind;
  text: string;
}

let seq = 0;
const listeners = new Set<(t: Toast) => void>();

export function notify(kind: Kind, text: string) {
  const t: Toast = { id: ++seq, kind, text };
  listeners.forEach((l) => l(t));
}

export function ok(text: string) {
  notify("ok", text);
}
export function err(text: string) {
  notify("err", text);
}

export function Toasts() {
  const [toasts, setToasts] = useState<Toast[]>([]);
  useEffect(() => {
    const on = (t: Toast) => {
      setToasts((prev) => [...prev, t]);
      setTimeout(() => setToasts((prev) => prev.filter((x) => x.id !== t.id)), 4000);
    };
    listeners.add(on);
    return () => {
      listeners.delete(on);
    };
  }, []);
  return (
    <>
      {toasts.map((t) => (
        <div key={t.id} className={`toast ${t.kind === "ok" ? "ok" : t.kind === "err" ? "err" : ""}`}>
          {t.text}
        </div>
      ))}
    </>
  );
}
