# Phase-1, Batch 1 — Foundations

> Minimum end-to-end loop between a Pi extension and a local daemon, on one
> machine, no network, no tmux, no coordinator.

## Goal

Prove the schema-driven contract works end-to-end at its smallest scope. A
running Pi process loads `pi-remote-ext`, the extension dials the daemon's
Unix socket, registers a session, exchanges heartbeats, and disconnects
cleanly when Pi shuts down. The daemon stays up across the extension's
lifecycle and tolerates disconnect / reconnect.

This batch deliberately defers everything that needs the network or external
infrastructure. When it's green, every later batch is a smaller delta.

## Issues in scope

| Issue | Component | Title |
|-------|-----------|-------|
| [#1](https://github.com/TheTechChild/pi-remote/issues/1) | ext | M1: Unix socket connection with reconnect |
| [#2](https://github.com/TheTechChild/pi-remote/issues/2) | ext | M2: Registration handshake |
| [#4](https://github.com/TheTechChild/pi-remote/issues/4) | ext | M4: Heartbeat loop |
| [#8](https://github.com/TheTechChild/pi-remote/issues/8) | daemon | M1: Unix socket listener with extension registration |

## Out of scope (deferred to later batches)

- ext M3 / M5 / M6 / M7 — event projection, project-name resolution,
  spawn-token correlation, graceful disconnect (M7 is folded into M1's
  socket teardown for this batch's purposes; the dedicated milestone closes
  later).
- daemon M2 (tmux), M3+ (coordinator), M5+ (pty), M7-M9 (suspend, reconnect).
- Coordinator entirely.
- TLS, CF Access, ntfy, push.

## Definition of done

A scripted local loop:

```sh
# terminal A
go run ./cmd/pi-remote-daemon

# terminal B
node scripts/manual-ext-harness.mjs       # to be written; see "Harness" below
```

Expected daemon stderr (slog JSON, abridged):

```
{"level":"INFO","msg":"socket listening","path":"/Users/.../daemon.sock"}
{"level":"INFO","msg":"register accepted","session_id":"...","pid":12345}
{"level":"DEBUG","msg":"heartbeat","session_id":"..."}   x3
{"level":"INFO","msg":"session disconnected","session_id":"...","reason":"session_shutdown"}
```

Then a reconnect probe:

```sh
# Terminal A: kill daemon (Ctrl-C), wait 5s, restart it
# Terminal B: leave the harness running
# Expected: extension reconnects with the same session_id and re-registers.
```

## Contract surface

**Schemas** — already in `pi-remote-spec/protocol/extension-daemon/`:
`register.json`, `register_ack.json`, `event.json`, `heartbeat.json`,
`disconnect.json`, `ping.json`. These are authoritative; both workstreams
read them via codegen.

**Generated code:**

- `pi-remote-ext/src/proto/extension-daemon/*.ts` — already committed.
- `pi-remote-daemon/internal/proto/extension-daemon/*.go` — **must be
  generated as the first task in Workstream B** (`bash
  pi-remote-daemon/scripts/codegen.sh`).

If either side ever needs to change a schema, that change ships in the same
PR as the regenerated outputs (SPEC.md § D25). For this batch, no schema
changes are expected.

## Workstream A — `pi-remote-ext` (#1, #2, #4)

**New files:**

```
pi-remote-ext/src/
├── socket.ts          # DaemonSocket: connect + reconnect + JSONL framing
├── register.ts        # registerWithDaemon(...) handshake
├── heartbeat.ts       # startHeartbeat(...): timer wrapper
├── project.ts         # placeholder for M5 (cwd parent only for now)
└── index.ts           # factory wires DaemonSocket -> register -> heartbeat
pi-remote-ext/test/
├── socket.test.ts     # connect, reconnect, EPIPE handling
├── register.test.ts   # ack accepted/rejected paths
├── heartbeat.test.ts  # vi.useFakeTimers cadence + cleanup
└── index.test.ts      # existing factory test, expanded
```

**Test surface (TDD-first; see `superpowers:test-driven-development`):**

| Behavior | Test |
|----------|------|
| Socket dial succeeds when daemon is up | `socket.test.ts` happy path |
| Socket dial fails when socket is missing → backoff 1s, 2s, 4s, ..., capped at 30s | clock fake |
| Mid-session daemon crash → reconnect with **same** `session_id` | `socket.test.ts` reconnect |
| `register` payload validates against generated `Register` type | `register.test.ts` |
| `register_ack { accepted: true }` → state becomes "registered" | `register.test.ts` |
| `register_ack { accepted: false, reason }` → log + close | `register.test.ts` |
| `PI_REMOTE_SPAWN_TOKEN=abc...` env → token in `register` | `register.test.ts` |
| No env var → `spawn_token` is `null` (or absent) | `register.test.ts` |
| Heartbeat fires every 10s | `heartbeat.test.ts` |
| Timer is cleared on disconnect | `heartbeat.test.ts` |
| Timer is cleared on socket close | `heartbeat.test.ts` |

**Key design choices:**

- **`DaemonSocket` is a small class around `node:net`.** It exposes
  `connect()`, `send(json)`, `on('message', cb)`, `on('reconnected', cb)`,
  `close()`. The reconnect loop lives inside it; consumers don't need to
  know the socket dropped.
- **`session_id` is generated once per Pi process** (UUIDv7 via
  `crypto.randomUUID()` for now — Node's built-in is v4; switch to a v7
  helper later if needed for sortability) and re-sent on every reconnect.
- **No event projection in this batch.** `index.ts` only handles
  `session_start` and `session_shutdown` from the Pi context. Other events
  (M3) hook in later.
- **Inputs we don't have yet:** the actual `tmux_target`. For this batch,
  send `"untmuxed:0.0"` as a placeholder; daemon M1 should accept any
  non-empty string. This unblocks the ext side without having to build the
  tmux integration first.

**Acceptance per issue (subset of the issue checklists, rest carry over):**

- #1: socket connects, reconnects with backoff, exits cleanly on
  `session_shutdown`. Tests cover all three.
- #2: `register` is sent and `register_ack` is required before any
  subsequent message. Tests cover accept and reject paths.
- #4: heartbeat at 10s cadence; timer cleanup verified.

## Workstream B — `pi-remote-daemon` (#8)

**First step: run codegen.**

```sh
bash pi-remote-daemon/scripts/codegen.sh
git add pi-remote-daemon/internal/proto
git commit -m "chore(daemon): generate wire types from pi-remote-spec/protocol/"
```

**New files:**

```
pi-remote-daemon/internal/
├── socket/
│   ├── listener.go       # Unix socket listener with single-instance check
│   ├── conn.go           # per-connection JSONL reader + writer
│   ├── listener_test.go
│   └── conn_test.go
├── session/
│   ├── registry.go       # sessionId -> Session, concurrent-safe
│   ├── session.go        # the Session struct (mostly already in SPEC § 7.5)
│   └── registry_test.go
└── (cmd/pi-remote-daemon/main.go updated to start the listener)
```

**Test surface:**

| Behavior | Test |
|----------|------|
| Listener binds to the configured socket path with mode 0600 | `listener_test.go` |
| Second daemon instance refuses to start (existing socket) | `listener_test.go` |
| Listener accepts a connection and reads JSONL frames | `conn_test.go` |
| Malformed JSON line → connection closed, registry untouched | `conn_test.go` |
| Frame larger than max (e.g., 1MB) → connection closed | `conn_test.go` |
| Valid `register` → entry added to registry, `register_ack` sent | `registry_test.go` |
| Duplicate `session_id` from different pid → `accepted=false`, `reason="ERR_DAEMON_DUPLICATE_SESSION_ID"` | `registry_test.go` |
| Same `session_id` from same pid (reconnect) → idempotent accept | `registry_test.go` |
| `heartbeat` updates `LastHeartbeat` | `registry_test.go` |
| `disconnect` removes the session from the registry | `registry_test.go` |
| Connection close without `disconnect` → registry retains session in state `ended` | `registry_test.go` |
| Concurrent registers + heartbeats are safe under `-race` | `registry_test.go` |

**Key design choices:**

- **Registry is a struct with `sync.RWMutex`** wrapping a
  `map[string]*Session`. No channels needed yet; channels go in when M3
  (coordinator multiplex) lands.
- **JSONL framing uses `bufio.Scanner` with a tuned `MaxScanTokenSize`**
  (1MB is plenty for control messages; pty bytes don't go through this
  socket).
- **Session state from the spec § 7.5 struct.** Fields the daemon doesn't
  need yet (`AttachedClients`, `LastSeq`) can be present but unused; this
  keeps the type stable for later milestones.
- **Heartbeat-timeout detection (3 missed = unresponsive)** is **deferred**.
  Implement it with a goroutine ticker in M3 alongside the coordinator
  reconnect path. M1 just records `LastHeartbeat`.

**`main.go` change:** stop being a pure "log version, exit 0" skeleton.
Start the listener, block on `signal.NotifyContext`, shut down gracefully
on SIGINT/SIGTERM.

## Manual harness for the smoke test

Create `pi-remote-ext/scripts/manual-ext-harness.mjs` — not part of the test
suite, just a way to drive the extension outside Pi while the rest of the
ecosystem catches up:

```js
#!/usr/bin/env node
// Drives pi-remote-ext from a fake Pi-like context. Intended for local
// development of Batch 1 before any tmux/coordinator wiring exists.
import factory from "../src/index.js";
const handlers = new Map();
factory({
  log: (m) => console.log(`[harness] ${m}`),
  on: (ev, h) => handlers.set(ev, h),
});
handlers.get("session_start")?.();
process.on("SIGINT", () => {
  handlers.get("session_shutdown")?.();
  setTimeout(() => process.exit(0), 200);
});
```

It belongs in `scripts/`, not `test/`, so it's never run by `vitest`.

## How to run the two workstreams in parallel

`git worktree` lets a single working clone host multiple branches checked
out at the same time. Two Claude Code sessions, one per worktree, in
separate terminals; they cannot collide on each other's files because each
worktree has its own working tree.

```sh
cd ~/projects/pi-remote

# Workstream A worktree (TypeScript / ext)
git worktree add ../pi-remote-ext-work -b ext/m1-m2-m4 main

# Workstream B worktree (Go / daemon)
git worktree add ../pi-remote-daemon-work -b daemon/m1 main

# Open one Claude Code session in each:
#   cd ../pi-remote-ext-work && claude
#   cd ../pi-remote-daemon-work && claude
```

When both are done:

```sh
cd ~/projects/pi-remote
gh pr create --base main --head ext/m1-m2-m4 ...
gh pr create --base main --head daemon/m1 ...
```

After both merge, run the integration smoke test from the section above on
a fresh checkout. If green, batch is done.

**Cleanup after merge** — auto-delete on GitHub removes the remote branches;
remove the local worktrees:

```sh
git worktree remove ../pi-remote-ext-work
git worktree remove ../pi-remote-daemon-work
```

(Or use the `superpowers:using-git-worktrees` skill — same workflow.)

### What you can NOT parallelize

- Anything that touches `pi-remote-spec/protocol/`. Those changes need to
  ship with the regenerated outputs from every consumer (D25), which means
  one PR touching every component. Not a concern for this batch — schemas
  don't need to change.
- Anything that touches the same file. The two workstreams above are
  scoped to disjoint subtrees (`pi-remote-ext/` and `pi-remote-daemon/`)
  and the only top-level file each touches is its own `package.json` (ext)
  / nothing (daemon). No file-level conflicts.

## Risks and gotchas

- **Pi extension API contract.** The extension docs don't fully spec the
  factory function's `ctx` shape. The skeleton in `src/index.ts` makes
  pessimistic assumptions (`ctx.on?.(...)`). If the real Pi API differs,
  this is the place we'll discover. Recommended check: install the
  extension via `pi install /absolute/path/to/pi-remote` (a local-path
  install per packages.md) and watch what Pi actually invokes.
- **`crypto.randomUUID()` is v4, not v7.** The spec (D17) calls for
  UUIDv7 because of timestamp-prefixed sortability. For Batch 1 it doesn't
  matter (sortability is only used by the broker), but flip to a v7 helper
  before the broker work in Batch 6.
- **Heartbeat-while-disconnected.** Make sure the heartbeat timer is
  cleared on socket close, not just on `disconnect`. A live timer trying
  to write to a dead socket will throw `EPIPE`. Test this.
- **Daemon socket path.** The default is `~/.pi-remote/daemon.sock`. The
  daemon should `os.MkdirAll(..., 0700)` for the parent dir on startup, and
  unlink any stale socket file (after the single-instance check finds no
  live listener).
- **Single-instance check race.** Two concurrent daemon starts could both
  find no listener and both try to bind. Use `flock(2)` on a sibling lock
  file (`~/.pi-remote/daemon.lock`) before binding, or accept the small
  race for v1 (file `Bind` will fail and the second instance exits with a
  useful error).

## Suggested PR rhythm

- **Workstream A:** one PR closing #1, #2, #4 together. Each milestone is
  small, and they share the test scaffolding (`vi.useFakeTimers`, the stub
  socket harness). Splitting them would multiply review cost.
- **Workstream B:** one PR closing #8. The first commit on the branch is
  the codegen output (chore: generate proto types).
- **No integration-test PR.** The smoke test is a local procedure, not
  committed code. Once Batch 2 starts (coordinator wiring), we'll add a
  proper end-to-end test harness.

## After this batch

The next batch (Batch 2) is the first cross-component dependency:

- ext M3 (event projection)
- daemon M3 / M4 (coordinator WebSocket client + multiplex)
- coordinator M1 / M2 (daemon and client WS endpoints with CF Access auth)

It can't be parallelized as cleanly because the daemon and coordinator
share the WebSocket schema and need to converge on the framing semantics
together. Plan for that one when this one's done.
