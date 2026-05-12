# pi-remote-ext

Pi extension that bridges each running [Pi](https://pi.dev) session into the
local `pi-remote-daemon` over a Unix socket.

## Overview

This is the **extension** half of [Pi Remote](../README.md) — a TypeScript
module loaded by Pi at startup. It connects to the daemon's Unix socket,
registers itself with session metadata, forwards structured events
(`agent_end`, `attention_dialog`, `tool_failure`, …), and maintains a
heartbeat. The daemon does the heavy lifting (pty mirroring, spawning,
coordinator wire) — this extension is intentionally thin.

See [`pi-remote-spec/SPEC.md`](../pi-remote-spec/SPEC.md) §§ 6, 10.1 for the
contract.

## Layout note

This directory is a Yarn workspace member of the [pi-remote
monorepo](../README.md). End users install the extension through the
monorepo root URL (see below); the root `package.json` carries the
`pi.extensions` manifest pointing back to `./pi-remote-ext/src/index.ts`,
and this workspace owns the actual dependency declarations.

## Install (end users)

```sh
pi install git:github.com/TheTechChild/pi-remote
```

Pi clones the monorepo, runs `npm install` at the root (npm's workspaces
resolve this workspace's deps automatically), reads the root `pi.extensions`
field, and loads `./pi-remote-ext/src/index.ts`. Pi auto-loads the extension
on every subsequent session.

## Build (developers)

From the repo root:

```sh
yarn install        # installs the workspace
yarn build          # delegates to this workspace
```

Or inside this workspace:

```sh
cd pi-remote-ext
yarn build
```

## Test

```sh
yarn test
yarn lint
yarn typecheck
```

## Codegen

Wire-protocol types live in [`src/proto/`](src/proto) and are generated from
the JSON Schema files in [`pi-remote-spec/`](../pi-remote-spec/) in this
monorepo. Regenerate with:

```sh
yarn codegen
```

The codegen script reads from the in-repo spec — no clone, no pin — because
the spec lives in the same repository.

## License

MIT — see [LICENSE](LICENSE).
