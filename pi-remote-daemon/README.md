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

For day-to-day work:

```sh
go test ./...
```

Before pushing a branch, run the **full pre-push checklist** — `gofmt`,
`go vet`, `golangci-lint`, `go test -race`, and (when the Dockerfile or
dependency graph has changed) a local `docker build`. See
[`../docs/go-local-dev.md`](../docs/go-local-dev.md) for the full
checklist and the one-liner.

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

## Known deviations

These are intentional, scoped trade-offs taken during phase-1 development.
Each has either a tracking issue or is documented for follow-up.

### LastSeq is not persisted across daemon restarts (SPEC § D18)

SPEC § D18 specifies that if the daemon truly loses its per-session `seq`
counter (unclean kill), it should resume at `last_known + 1000` to avoid
collision with cached coordinator entries. The daemon's M3+M4
implementation keeps `LastSeq` in memory only and starts at 1 on every
process boot. This is acceptable in phase-1 because the coordinator's
broker (and its session-keyed cache) lands in a later batch; until then
there is nothing on the coordinator side that would observe a collision.

Disk-buffered persistence of `LastSeq` is filed as future hardening. The
broker batch will revisit.

### v1 has no daemon-minted UUIDs (SPEC §§ D17, 22.2)

SPEC § D17 mandates UUIDv7 for all minted IDs. An audit performed during
Batch 2 (see #46) confirmed that **the daemon mints no UUID-shaped IDs in
v1's design**:

- `session_id` is ext-minted; the daemon receives it via `register` and
  passes it through opaquely. The ext-side v4 → v7 migration is tracked
  in #46.
- `request_id` is app-minted (SPEC § 15 step 2); the daemon echoes it
  verbatim on `spawn_response`.
- `machine_id` is set in `daemon.toml`, not runtime-minted.
- `spawn_token` is `crypto/rand` 16-byte hex per SPEC § D6 — a distinct
  ID class, not a UUID.

`github.com/google/uuid` v1.6.x remains on SPEC § 22.2's approved-deps
list against future need but is not currently imported by the daemon.
When a future change introduces a daemon-minted UUID, the dep is added
then, governed by [`docs/go-dependencies.md`](../docs/go-dependencies.md).

### Config loader is Phase-0 stub; CLI flags are a dev affordance (#48)

`internal/config/config.go` currently returns a hardcoded `*Config`
rather than parsing the TOML file SPEC § 7.3 specifies. The real
loader is tracked in #48 and lands as a separate piece of work.

Until then, four CLI flags on `cmd/pi-remote-daemon` override the
bare-minimum operator-supplied values so local development against a
stub coordinator (`pi-remote-coordinator -auth=stub`) works without
populating `/etc/pi-remote/`:

```sh
go run ./cmd/pi-remote-daemon \
  -coordinator-url=ws://localhost:8080/v1/daemon \
  -machine-id=test-machine \
  -service-token-id-file=/tmp/pi-remote-test-id \
  -service-token-secret-file=/tmp/pi-remote-test-secret
```

When #48 lands, these flags stay as the top of the
flag > env > file > defaults precedence chain documented in that
issue's design.

## Deploy

- macOS: see [`deploy/install-macos.sh`](deploy/install-macos.sh) and
  [`deploy/launchd/dev.pi-remote.daemon.plist`](deploy/launchd/dev.pi-remote.daemon.plist).
- Linux: see [`deploy/install-linux.sh`](deploy/install-linux.sh) and
  [`deploy/systemd/pi-remote-daemon.service`](deploy/systemd/pi-remote-daemon.service).

## License

MIT — see [LICENSE](LICENSE).
