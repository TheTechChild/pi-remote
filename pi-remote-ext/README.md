# pi-remote-ext

Pi extension that bridges each running [Pi](https://pi.dev) session into the
local `pi-remote-daemon` over a Unix socket.

## Overview

This is the **extension** half of [Pi Remote](https://github.com/TheTechChild/pi-remote-spec) —
a TypeScript module loaded by Pi at startup. It connects to the daemon's Unix
socket, registers itself with session metadata, forwards structured events
(`agent_end`, `attention_dialog`, `tool_failure`, …), and maintains a
heartbeat. The daemon does the heavy lifting (pty mirroring, spawning,
coordinator wire) — this extension is intentionally thin.

See [`pi-remote-spec/SPEC.md`](https://github.com/TheTechChild/pi-remote-spec/blob/main/SPEC.md) §§ 6,
10.1 for the contract.

## Setup

```sh
pi install git:github.com/TheTechChild/pi-remote-ext
```

Pi will auto-load the extension on every subsequent session.

## Build

```sh
yarn install
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
the JSON Schema files in
[`pi-remote-spec`](https://github.com/TheTechChild/pi-remote-spec). Regenerate with:

```sh
yarn codegen
```

The pinned spec commit is recorded in
[`scripts/spec-version.txt`](scripts/spec-version.txt). Bumping the pin is a
deliberate PR (see SPEC.md § D21).

## License

MIT — see [LICENSE](LICENSE).
