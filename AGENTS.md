# AGENTS.md — working rules for AI agents on llmctl

## Read first
1. `docs/PLAN.md` — roadmap, build order, current item.
2. `FEATURES.md` — feature register; update statuses as you work.
3. `docs/ARCHITECTURE.md` — how the system fits together.
4. `docs/PRODUCT.md` — what's shipped and the design principles.

## How to work
- **Iterative, small chunks.** Pick ONE item from PLAN.md (top of the build
  order), make an incremental change, verify it, then move to the next.
- **Do not overthink.** Prefer simple, easy-to-understand, easy-to-implement
  designs. If a design needs a paragraph to explain, simplify it.
- **Verify before claiming done.** Build (`go build ./...`), test
  (`go test ./...`), and for user-facing changes actually run the daemon and
  exercise the feature (curl the API / use the UI). A feature is COMPLETED
  only when verified, not when code is written.
- **Update the docs as you go.**
  - Feature status changes → `FEATURES.md` (and PLAN.md table if it's a roadmap item).
  - Architecture changes → `docs/ARCHITECTURE.md`.
  - Shipped behavior / product decisions → `docs/PRODUCT.md`.
  - New planned feature → add a row to `FEATURES.md` with the next free ID.

## Project facts
- Go module `llmctl`, stdlib only, no cgo. Go 1.27 toolchain.
- Single binary: `go build -o bin/llmctl.exe ./cmd/llmctl` (Windows dev
  host; use `llmctl` without .exe on linux/darwin).
- One daemon, one port (default `:8080`): `/v1/*` gateway, `/api/v1/*`
  management API, `/` web UI.
- State: `~/.llmctl/state.json` (atomic writes + lock). Override with
  `LLMCTL_HOME`.
- Tests: `go test ./...`. The stdio tests compile a tiny fake backend with
  `go build` — they need the Go toolchain available.
- The embedded UI is `internal/ui/index.html` (single file, no CDN deps —
  keep it that way so it works offline).
- All UI features must be backed by `/api/v1` endpoints. If you add a UI
  action, add the API first.

## Conventions
- New packages under `internal/`. Keep them small and single-purpose.
- Errors: wrap with context (`fmt.Errorf("...: %w", err)`).
- No new external dependencies without a strong reason (stdlib-first).
- Windows is a first-class target: no unix-only syscalls without a
  `//go:build` pair (see `internal/supervisor/proc_*.go`).
- Don't commit secrets. API keys are stored as sha256 hashes only.

## Definition of done (per feature)
1. Code builds: `go build ./...` clean.
2. Tests pass: `go test ./...` clean.
3. Feature exercised for real (API call / UI action / CLI command).
4. `FEATURES.md` status updated to COMPLETED.
5. Relevant doc (ARCHITECTURE/PRODUCT/README) updated if behavior changed.
