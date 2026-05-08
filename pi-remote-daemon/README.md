# pi-remote-daemon

Per-machine Go daemon that bridges Pi sessions running on the local box up
to the [pi-remote coordinator](https://github.com/TheTechChild/pi-remote-coordinator).

## Overview

This is the **daemon** half of [Pi Remote](https://github.com/TheTechChild/pi-remote-spec) —
one process per coding machine. It accepts extension registrations on a Unix
socket, drives a `tmux -CC` control-mode client to mirror pty bytes from
every registered Pi pane, multiplexes structured events plus pty bytes onto
a single WebSocket out to the coordinator (CF-Access service-token auth),
detects machine suspend / resume, and spawns new Pi sessions on coordinator
request.

See [`pi-remote-spec/SPEC.md`](https://github.com/TheTechChild/pi-remote-spec/blob/main/SPEC.md) §§ 7,
10.2, 11, 14 for the contract.

## Setup

Phase-0 builds an empty skeleton. Use the `deploy/install-*.sh` scripts to
provision launchd / systemd later.

## Build

```sh
go build ./...
```

The binary lands at `./pi-remote-daemon` by default. Build with version stamping:

```sh
go build -ldflags "-X main.Version=$(git rev-parse --short HEAD)" ./cmd/pi-remote-daemon
```

## Test

```sh
go test ./...
golangci-lint run
```

## Codegen

Wire-protocol types live in [`internal/proto/`](internal/proto) and are generated
from the JSON Schema files in [`pi-remote-spec`](https://github.com/TheTechChild/pi-remote-spec).
Regenerate with:

```sh
bash scripts/codegen.sh
```

The pinned spec commit is recorded in
[`scripts/spec-version.txt`](scripts/spec-version.txt). Bumping the pin is a
deliberate PR (see SPEC.md § D21).

## Deploy

- macOS: see [`deploy/install-macos.sh`](deploy/install-macos.sh) and
  [`deploy/launchd/dev.pi-remote.daemon.plist`](deploy/launchd/dev.pi-remote.daemon.plist).
- Linux: see [`deploy/install-linux.sh`](deploy/install-linux.sh) and
  [`deploy/systemd/pi-remote-daemon.service`](deploy/systemd/pi-remote-daemon.service).

## License

MIT — see [LICENSE](LICENSE).
