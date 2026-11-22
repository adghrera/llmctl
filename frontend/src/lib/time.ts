// Shared relative-time formatting (F237): "42s ago", "7m ago", "3h ago", "5d ago".
export function ago(t: string | number | Date): string {
  const s = Math.max(0, Math.floor((Date.now() - new Date(t).getTime()) / 1000));
  if (s < 60) return `${s}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}
