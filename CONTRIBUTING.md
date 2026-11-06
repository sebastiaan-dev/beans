# Development

Tools (Go, Node.js, pnpm) are managed with [mise](https://mise.jdx.dev): run
`mise install`, then `mise setup` to install dependencies and generate code.

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

Two checks are deliberately limited while the code base is cleaned up:

- `gofmt` and `prettier` only check files changed compared to the base commit
  (locally: the merge base with `main`, plus uncommitted files), because many
  existing files aren't formatted yet. `mise run fmt` formats everything.
- `staticcheck` fails only on findings that aren't listed in
  `.github/staticcheck-baseline.txt`. Fix new findings instead of adding them.

`release.yml` (GoReleaser + Homebrew tap) only runs in `hmans/beans`.
