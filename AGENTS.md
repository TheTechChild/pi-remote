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
