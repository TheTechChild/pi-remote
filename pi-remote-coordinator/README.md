# pi-remote-coordinator

Go service that brokers between every coding-machine's
[`pi-remote-daemon`](https://github.com/TheTechChild/pi-remote-daemon) and
every Android client running
[`pi-remote-android`](https://github.com/TheTechChild/pi-remote-android).
Designed to run as a Docker container on UnraidOS.

## Overview

This is the **coordinator** half of [Pi Remote](https://github.com/TheTechChild/pi-remote-spec).
It accepts inbound WebSockets from daemons (CF service-token auth) and clients
(CF Access JWT auth), maintains an in-memory per-session ring buffer (50MB
total cache cap with global LRU), forwards events between them, encrypts push
payloads with `crypto_box`, and POSTs them to a sibling self-hosted ntfy
server.

See [`pi-remote-spec/SPEC.md`](https://github.com/TheTechChild/pi-remote-spec/blob/main/SPEC.md) §§ 8,
10.2, 10.3, 11, 18, 19 for the contract.

## Setup (UnraidOS)

```sh
docker compose -f deploy/docker-compose.yaml up --build -d
curl http://localhost:8080/v1/health  # → 200 OK
```

This starts the coordinator on `:8080` and an ntfy sidecar on `:8081` on a
shared Docker network (SPEC.md § D15).

## Build

```sh
go build ./...
```

## Test

For day-to-day work:

```sh
go test ./...
```

Before pushing a branch, run the **full pre-push checklist** — `gofmt`,
`go vet`, `golangci-lint`, `go test -race`, and (when the Dockerfile or
dependency graph has changed) a local `docker build`. Skipping any of
these has bitten the project at least twice. See
[`../docs/go-local-dev.md`](../docs/go-local-dev.md) for the full
checklist and the one-liner.

## Codegen

Wire-protocol types live in [`internal/proto/`](internal/proto). Regenerate with:

```sh
bash scripts/codegen.sh
```

The pinned spec commit is recorded in [`scripts/spec-version.txt`](scripts/spec-version.txt).

## License

MIT — see [LICENSE](LICENSE).
