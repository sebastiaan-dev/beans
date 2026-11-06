# Development

Tools (Go, Node.js, pnpm) are managed with [mise](https://mise.jdx.dev): run
`mise install`, then `mise setup` to install dependencies and generate code.

pnpm is pinned to an exact version in `mise.toml`; CI's
`.github/actions/setup-frontend` reads it from there, so bump it only in
`mise.toml`. Frontend pnpm settings live in `frontend/pnpm-workspace.yaml`
(pnpm 11+ ignores non-auth settings in `.npmrc`): dependencies that may run
install scripts are listed under `allowBuilds`; when `pnpm install` fails with
`ERR_PNPM_IGNORED_BUILDS` after adding a dependency, add it there.

| Task            | What it does                                                                |
| --------------- | --------------------------------------------------------------------------- |
| `mise build`    | Build `./beans`, `./beans-serve` and `./beans-tui` (with embedded frontend) |
| `mise codegen`  | Regenerate GraphQL code (after changing `schema.graphqls` or operations)    |
| `mise run fmt`  | Format all Go (`gofmt`) and frontend (`prettier`) files                     |
| `mise run lint` | The lint checks CI runs (see below)                                         |
| `mise test`     | All tests: `test:go`, `test:frontend` (vitest) and `test:e2e` (Playwright)  |

## CI

`.github/workflows/test.yml` runs on pushes to `main`, pull requests and
manually (`workflow_dispatch`). Every job must pass:

- **lint** (`mise run lint:go` + `mise run check:codegen`): `gofmt`, `go vet`,
  `staticcheck` and a check that generated code is up to date.
- **test-go**: `go test -race ./...` on Linux (with worktrunk installed, so the
  worktrunk backend tests run).
- **cross**: `go build ./cmd/...` and `go vet ./...` for Linux, macOS and Windows.
- **frontend** (`mise run lint:frontend` + vitest + build): `svelte-check`,
  vitest, `prettier --check`, `vite build`.
- **e2e**: the Playwright suite against the embedded frontend.

- `gofmt` and `prettier` check every file; run `mise run fmt` before committing
  (`--changed` checks only files changed since the base commit).
- `staticcheck` fails only on findings that aren't listed in
  `.github/staticcheck-baseline.txt` (pre-existing ones). Fix new findings
  instead of adding them.

`release.yml` (GoReleaser + Homebrew tap) only runs in `hmans/beans`.
