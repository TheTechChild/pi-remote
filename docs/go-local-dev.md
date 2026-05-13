# Local development checklist for Go components

This doc covers the pre-push checklist for the two Go workspaces in this
monorepo:

- [`pi-remote-coordinator/`](../pi-remote-coordinator/)
- [`pi-remote-daemon/`](../pi-remote-daemon/)

The JavaScript side has its own checklist in
[`package-management.md`](package-management.md); Android has its own in
[`pi-remote-android/`](../pi-remote-android/) (when it lands).

## Why this exists

`go test ./...` is **not** the full CI surface. Treating it as such has
already cost us two round-trips of broken PRs:

| What slipped past `go test` | What CI ran that caught it |
| --- | --- |
| Unchecked `Close()` return in tests | `golangci-lint` (default `standard` set includes `errcheck`) |
| Trailing blank line, mis-aligned tab | `gofmt -l` inside `golangci-lint` |
| `go.sum` missing from `deploy/Dockerfile` after adding a new module dep | `docker build` step of `coordinator / build` |

The fixes were trivial; the round-trip wasn't. This doc is the
pre-push routine that would have caught both locally.

## The pre-push checklist

Run from the workspace directory (`pi-remote-coordinator/` or
`pi-remote-daemon/`). All five must pass before pushing a feature branch.

```sh
# 1. Build everything.
go build ./...

# 2. Static analysis.
go vet ./...

# 3. Formatting. MUST be silent (an empty result list).
gofmt -l .

# 4. Lint. Uses the workspace's .golangci.yml.
golangci-lint run ./...

# 5. Tests under the race detector. -count=1 disables caching;
#    use -count=3 if you've touched any concurrent code.
go test -race -count=1 ./...
```

If your change touches the Dockerfile, `go.mod`, `go.sum`, the
`deploy/` directory, or adds/removes any external dependency, also run:

```sh
# 6. Build the production container image.
docker build -f deploy/Dockerfile -t <workspace>:local .

# 7. Smoke the container's entrypoint.
docker run -d --name smoke -p 18080:8080 <workspace>:local
sleep 2
curl -fsS http://127.0.0.1:18080/v1/health   # → {"status":"ok"} on coordinator
docker rm -f smoke
```

CI runs the equivalent of (6) + (7) on every PR that touches the
workspace. Running it locally first avoids the 90-second feedback loop.

## Installing the tools

### `gofmt`

Ships with the Go toolchain. Nothing to install.

### `golangci-lint`

Pin to the same major version CI uses. CI tracks `latest` via
[`golangci/golangci-lint-action@v8`](../.github/workflows/ci.yml); install
the same way locally:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

Confirm it lands on your `PATH`:

```sh
golangci-lint version
# golangci-lint has version 2.x.x ...
```

If `which golangci-lint` comes up empty, prepend `$(go env GOPATH)/bin`
to your `PATH`.

### `docker`

Docker Desktop or any compatible engine (colima, podman in Docker-compat
mode). CI uses `docker/setup-buildx-action@v3` with buildx; any modern
Docker client supports the `docker build` invocation above.

## Why each step matters

### `gofmt -l .`

`gofmt -l` lists files that would be reformatted. An empty result is the
pass signal. Anything listed will fail CI's `golangci-lint` run (the
`gofmt` formatter is enabled in both workspaces' `.golangci.yml`).

If the list isn't empty, apply the fix:

```sh
gofmt -w .
```

### `golangci-lint run`

The default linter set (declared via `default: standard` in each
workspace's `.golangci.yml`) includes:

- `errcheck` — catches unchecked error returns. The usual offender in
  test code is `defer resp.Body.Close()` and `defer conn.Close(...)`.
  Wrap them: `defer func() { _ = resp.Body.Close() }()`.
- `govet` — same as `go vet` but stricter under the linter harness.
- `staticcheck`, `unused`, `ineffassign`, `misspell` — see each
  workspace's `.golangci.yml` for the full enabled list.

### `go test -race`

Race conditions in WebSocket handlers and in-memory registries surface
*only* under `-race`. Several real races have already been caught this
way during development (one in Workstream C forced the registry `Get`
methods to return value-snapshots instead of aliased pointers — see
commit history of `pi-remote-coordinator/internal/sessions/registry.go`).
`-count=1` defeats the test cache; `-count=3` is good for flushing out
flakes in goroutine-heavy code.

### `docker build` + smoke

The production container is built from a separate Dockerfile that
deliberately scopes which files it copies into the build stage
(`go.mod`, `go.sum`, `cmd/`, `internal/`). It is **easy** to add a new
file at the workspace root — a new top-level package, a new asset — and
forget to add it to the `COPY` list. `go build ./...` in your shell
won't catch this because your shell sees the whole repo; the Dockerfile
build context doesn't.

The same pitfall applies to `go.sum`: a fresh `go get` writes both
`go.mod` and `go.sum`, but if the Dockerfile only copies one of them
the in-container `go build` fails with `missing go.sum entry for
module ...`.

## Quick-reference: the one-liner

Paste into your shell from the workspace directory:

```sh
go build ./... && \
  go vet ./... && \
  test -z "$(gofmt -l .)" && \
  golangci-lint run ./... && \
  go test -race -count=1 ./...
```

If that exits clean, your branch will survive CI's `coordinator / build`
or `daemon / build` job — modulo the Dockerfile case, which only matters
when you've touched build inputs.
