# llmctl — Feature Roadmap

Working style: **iterative, small chunks**. Pick the next item, make an
incremental change, verify it, update status in `FEATURES.md`, move on.
Prefer simple, easy-to-understand designs. Don't overthink.

## Build order (top = next)

| # | Priority | Category | Title | Status |
|---|----------|----------|-------|--------|
| 1 | P0 | quality | Fix stdio protocol test + full test suite green | COMPLETED |
| 2 | P0 | e2e | End-to-end verification of new architecture (daemon up, install, start, chat via API) | COMPLETED |
| 3 | P0 | ui | Chat interface: select model, chat with the LLM (streaming) | COMPLETED |
| 4 | P1 | docs | Update README for v0.2 (single port, management API, UI, plugins, stdio) | COMPLETED |
| 5 | P1 | docker | Dockerfile + .dockerignore; verify `docker build` works | COMPLETED |
| 6 | P1 | platform | Cross-compile check: linux/amd64, darwin/arm64 | COMPLETED |
| 7 | P2 | backends | Example plugin backend JSON (documented) | COMPLETED |
| 8 | P1 | ui | **Proper UI** — scaffold Vite+React+TS frontend + build→embed pipeline | COMPLETED |
| 9 | P1 | ui | **Proper UI** — typed API client + central state store | COMPLETED |
| 10 | P1 | ui | **Proper UI** — port all pages + routing + nav shell | COMPLETED |
| 11 | P2 | ui | **Proper UI** — reusable component kit + theme tokens | COMPLETED |
| 12 | P2 | ui | **Proper UI** — cutover: serve new UI, remove old index.html, verify | COMPLETED |
| 13 | P2 | ui | UI polish: error toasts, job history, confirm dialogs | COMPLETED |
| 14 | P2 | api | Per-key rate limits / quotas | COMPLETED |
| 15 | P3 | backends | Load-balanced replicas of one model | COMPLETED |
| 16 | P3 | models | Model catalog page (curated GGUF, one-click install) | COMPLETED |
| 17 | P4 | api | WebSocket passthrough | COMPLETED |
| 18 | P4 | ui | Instance log viewer (tail stderr.log via API) | COMPLETED |
| 19 | P5 | ops | Auto-start configured instances on boot | COMPLETED |
| 20 | P5 | api | Metrics endpoint | COMPLETED |

## Proper UI — design note (shipped)

The UI is now a real **React + Vite + TypeScript** SPA, replacing the old
single-file vanilla-JS page (removed at cutover, F020). It is a **dev-time
build dependency only** — the shipped Go binary stays self-contained.

- **Layout**: `frontend/` (Vite project) → `npm run build` →
  `internal/ui/dist/` → embedded by the Go binary via `//go:embed all:dist`.
  `dist/` is committed so `go build` works out of the box (no npm needed).
- **Structure**: `src/api/client.ts` (typed `/api/v1` + `/v1` chat SSE),
  `src/state/store.tsx` (React context store: load + 10s poll + SSE job
  subscription), `src/ui/toast.tsx` (toast kit), `src/pages/*` (one per tab),
  `App.tsx` (router + nav shell).
- **Theme**: dark theme as CSS design tokens (`:root` vars) + a reusable
  component kit (btn/badge/table/progress/toast/keybox/chat) in `index.css`.
- **SPA serving**: the Go handler serves static assets and falls back to
  `index.html` for any other path, so client-side routing works on refresh
  and deep links (e.g. `/chat`).

Chunks (FEATURES.md F016–F020) all COMPLETED: scaffold → API+store → port
pages → component kit → cutover.

## Rules

- Every planned feature lives in `FEATURES.md` with a stable ID (F001…).
- Status changes happen **when the work is done and verified**, not when
  started (IN-PROGRESS is allowed while actively working).
- Architecture changes → `docs/ARCHITECTURE.md`.
- Shipped behavior / product decisions → `docs/PRODUCT.md`.
- One item at a time. Small commits. Verify before marking COMPLETED.
