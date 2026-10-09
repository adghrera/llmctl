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
| 7 | P2 | backends | Example plugin backend JSON (documented, in docs/) | IN-PROGRESS |
| 8 | P2 | ui | UI polish: error toasts on failed actions, job history section | NOT-STARTED |
| 9 | P2 | api | Per-key rate limits / quotas (currently global per-client) | NOT-STARTED |
| 10 | P3 | backends | Load-balanced replicas of one model (round-robin instances) | NOT-STARTED |
| 11 | P3 | models | Model catalog page: curated list of popular GGUF models, one-click install | NOT-STARTED |
| 12 | P4 | api | WebSocket passthrough for backends that support it | NOT-STARTED |
| 13 | P4 | ui | Instance log viewer (tail stderr.log via API) | NOT-STARTED |
| 14 | P5 | ops | Auto-start configured instances on daemon boot | NOT-STARTED |
| 15 | P5 | api | Metrics endpoint (requests, tokens, latency) | NOT-STARTED |

## Rules

- Every planned feature lives in `FEATURES.md` with a stable ID (F001…).
- Status changes happen **when the work is done and verified**, not when
  started (IN-PROGRESS is allowed while actively working).
- Architecture changes → `docs/ARCHITECTURE.md`.
- Shipped behavior / product decisions → `docs/PRODUCT.md`.
- One item at a time. Small commits. Verify before marking COMPLETED.
