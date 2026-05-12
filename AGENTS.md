# AGENTS.md

## Package management

This monorepo uses **Yarn 4 (Berry)** with workspaces. The Pi extension's
dependencies live in [`pi-remote-ext/package.json`](pi-remote-ext/package.json);
the root manifest is the pi-install entry point and the workspace
orchestrator. Run scripts from the repo root (`yarn build`, `yarn test`,
`yarn lint`, `yarn codegen`) or from inside the workspace
(`cd pi-remote-ext && yarn test`).

See [`docs/package-management.md`](docs/package-management.md) for the full
rationale, the layered-override strategy, and the upgrade procedure.

## Go dependencies

The daemon (`pi-remote-daemon/`) and coordinator (`pi-remote-coordinator/`)
are independent Go modules. Direct dependencies are governed by SPEC §§ 22.2
/ 22.3 (the approved-deps tables) and pinned exact-patch in `go.mod`.

See [`docs/go-dependencies.md`](docs/go-dependencies.md) for the full rules,
the add-a-dep / upgrade-a-dep procedures, and the toolchain pinning policy.

## Go local development

The two Go workspaces (`pi-remote-coordinator/` and `pi-remote-daemon/`)
have a pre-push checklist that goes beyond `go test ./...`. CI runs
`gofmt`, `golangci-lint`, `go test -race`, **and** a `docker build` of
the production Dockerfile — each of which can fail independently of the
test suite. Run them locally before pushing.

See [`docs/go-local-dev.md`](docs/go-local-dev.md) for the full
checklist, why each step matters, and the one-liner to paste before
`git push`.
