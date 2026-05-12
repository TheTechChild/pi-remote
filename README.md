# pi-remote

Pi Remote is a session-sharing system for the [Pi](https://pi.dev) coding
agent. It lets your coding machines publish live Pi sessions to a coordinator
service running on UnraidOS, and lets your Android devices attach to those
sessions for monitoring, push notifications when an agent needs input, and
bidirectional terminal interaction.

This repository is a **monorepo** containing all five components plus the
authoritative spec, JSON Schemas, fixtures, scenarios, and ADRs.

## Components

| Path | What | Stack |
|------|------|-------|
| [`pi-remote-spec/`](pi-remote-spec/) | Authoritative spec, wire-protocol JSON Schemas, fixtures, scenarios, ADRs. The single source of truth. | Markdown, JSON Schema (Draft 2020-12) |
| [`pi-remote-ext/`](pi-remote-ext/) | TypeScript Pi extension that bridges each Pi session to the local daemon. | Node.js 22, TypeScript, biome, vitest |
| [`pi-remote-daemon/`](pi-remote-daemon/) | Per-machine Go daemon that mirrors pty bytes via `tmux -CC` and forwards to the coordinator. | Go 1.25 |
| [`pi-remote-coordinator/`](pi-remote-coordinator/) | Go coordinator on UnraidOS, brokers between daemons and Android clients. | Go 1.25, Docker, distroless |
| [`pi-remote-android/`](pi-remote-android/) | Native Kotlin Android app with vendored Termux terminal modules + UnifiedPush. | Kotlin 2.1, Compose, AGP 8.7, NDK 27 |

## Read the spec first

The full technical specification lives at [`pi-remote-spec/SPEC.md`](pi-remote-spec/SPEC.md).
Wire-protocol contracts in [`pi-remote-spec/protocol/`](pi-remote-spec/protocol/) drive
codegen in every consumer.

## Installing the Pi extension

The root `package.json` is the pi-package manifest and the Yarn workspace
orchestrator. The extension's actual dependencies live in
[`pi-remote-ext/package.json`](pi-remote-ext/package.json); the root
manifest's role is to declare the `pi.extensions` entry point, list the
workspaces, and pin the package manager. See ADR-0005 in
[`pi-remote-spec/adrs/`](pi-remote-spec/adrs/) for the full rationale and
[`docs/package-management.md`](docs/package-management.md) for the
mechanics.

```sh
pi install git:github.com/TheTechChild/pi-remote
# or pin to a release tag:
pi install git:github.com/TheTechChild/pi-remote@v1.0.0
```

Pi clones the whole repo, runs `npm install` at the root (npm understands
the `workspaces` field and installs the ext workspace's deps into the root
`node_modules/`), reads the `pi.extensions` field, and loads
`./pi-remote-ext/src/index.ts`. The other components (daemon, coordinator,
android) live in the same repo but aren't loaded by Pi.

## Building

Each component builds with its own toolchain. The TS extension uses Yarn 4
(Berry) workspaces; see [`AGENTS.md`](AGENTS.md) for the short version and
[`docs/package-management.md`](docs/package-management.md) for everything
else.

```sh
# Extension (from the repo root — delegates to the pi-remote-ext workspace)
yarn install
yarn build && yarn test && yarn lint

# Or directly inside the workspace
( cd pi-remote-ext && yarn build && yarn test && yarn lint )

# Daemon
( cd pi-remote-daemon && go build ./... && go test ./... )

# Coordinator
( cd pi-remote-coordinator && go build ./... && go test ./... )
( cd pi-remote-coordinator && docker compose -f deploy/docker-compose.yaml up --build )

# Android
( cd pi-remote-android && ./gradlew :app:assembleDebug )
```

## Codegen

Wire-protocol types in each component's `proto/` directory are generated from
[`pi-remote-spec/protocol/`](pi-remote-spec/protocol/). Each component has a
`scripts/codegen.sh` that reads from the in-repo spec (no clone, no pin):

```sh
bash pi-remote-ext/scripts/codegen.sh
bash pi-remote-daemon/scripts/codegen.sh
bash pi-remote-coordinator/scripts/codegen.sh
bash pi-remote-android/scripts/codegen.sh
```

A schema change in `pi-remote-spec/protocol/` and the regenerated outputs
should ship in the same PR (per SPEC.md § D25).

## CI

A single GitHub Actions workflow at
[`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs path-filtered
jobs:

- Changes under `pi-remote-spec/protocol/` or `pi-remote-spec/fixtures/` →
  schema validation job.
- Changes under `pi-remote-spec/protocol/**` also fan out to the four
  consumers' build jobs.
- Component-only changes (`pi-remote-daemon/**`, etc.) trigger only that
  component's job.
- Branch protection requires the aggregating `gate` job to pass.

## License

MIT — see [LICENSE](LICENSE). The vendored Termux modules under
`pi-remote-android/vendor/` are Apache 2.0; see
[`pi-remote-android/vendor/NOTICE`](pi-remote-android/vendor/NOTICE).
