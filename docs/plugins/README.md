# Example plugin backends

These are **templates** for pluggable backends. Copy one to
`~/.llmctl/plugins/<id>.json` (or paste it into the UI's "Add plugin backend"
box, or `llmctl backends add <file>`), then `llmctl backends install <id>`.
No recompile — plugins are data.

## Fields

| Field | Required | Notes |
|---|---|---|
| `id` | yes | unique backend id (used in `start`, `install`) |
| `name` | no | display name |
| `description` | no | shown in the UI |
| `transport` | yes | `http` (OpenAI-compatible server) or `stdio` (llmctl-stdio/1) |
| `kind` | yes | `process` (llmctl launches it) or `attach` (you run it) |
| `binary` | process | executable name (resolved in the backend dir or PATH) |
| `args` | no | launch args; supports `{model}`, `{dir}`, `{var:key}`, `{port}` |
| `env` | no | extra env vars for the process |
| `attachUrl` | attach+http | fixed endpoint, e.g. `http://127.0.0.1:11434` |
| `healthPath` | no | default `/health` |
| `healthTimeoutS` | no | default 120 |
| `apiBasePath` | no | OpenAI root, e.g. `/v1` |
| `stdioReadyTimeoutS` | stdio | how long to wait for the hello line |
| `stopGraceS` | no | seconds before force-kill (default 10) |
| `install` | no | how to download the runtime (see below) |

### `install` kinds

- **`url`** — single binary. `url` may contain `{os}` (win/linux/macos),
  `{arch}` (amd64/arm64), `{ext}` (exe or empty).
- **`github-release`** — `repo` (owner/repo), `binaryName`, optional
  `assetMatch` (GOOS → asset substring), `extract` (`zip`/`tar.gz`/empty).
- **`files`** — list of `{name, url}` to fetch into the backend dir.

## Files here

- `http-github-release.json` — HTTP process backend installed from a GitHub
  release (the common case: a server binary that speaks OpenAI HTTP).
- `http-url.json` — HTTP process backend installed from a direct URL.
- `stdio-process.json` — stdio process backend (llmctl-stdio/1 over pipes).
- `attach-remote.json` — attach backend for a server you run yourself.
