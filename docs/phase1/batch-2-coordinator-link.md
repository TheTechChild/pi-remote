# Phase-1, Batch 2 — Coordinator link

> First cross-component dependency: a real Pi event projection flows ext →
> daemon → coordinator over an authenticated WebSocket, and a CF Access-
> authenticated client can open a WebSocket and say hello. End-to-end
> shape is in place; broker, ring buffer, and live fan-out come next.

## Goal

Prove the **upstream half** of the live-stream pipeline:

1. A Pi event fires inside the extension (e.g. `agent_start`).
2. `events.ts` projects it to an `event` JSONL frame and hands it to the
   daemon socket.
3. The daemon's session multiplex tags it with the per-session monotonic
   `seq`, wraps it as `session_event`, and writes it to the coordinator
   WebSocket.
4. The coordinator authenticates the daemon via CF service token, accepts
   `machine_register`, and ingests `session_started` / `session_event` /
   `session_pty` / `session_state_change` / `session_ended` /
   `machine_suspending` / `session_resume` / `spawn_response` frames into
   an in-memory machines + sessions registry.
5. A separate WebSocket on `/v1/client` authenticates a CF Access JWT,
   accepts `client_hello`, and registers the client into the clients
   registry. **Fan-out to attached clients (ring buffer, replay, `attach`/
   `detach`) is explicitly deferred — that's Batch 3 / broker work.**

The smallest provable loop: with all three processes running locally and
fake CF middleware, `console.log("simulated agent_start")` in the harness
produces a structured `{"type":"session_event","kind":"agent_start",...}`
line in the coordinator's debug log, with `seq=1` for that session.

This is the first batch where a schema change (if needed) ships as one
cross-component PR per D25. We do not expect new schemas — every message
listed above already has a JSON Schema under
`pi-remote-spec/protocol/daemon-coordinator/` and
`pi-remote-spec/protocol/coordinator-app/`. We *will* be regenerating
proto outputs into the daemon and coordinator for the first time.

## Issues in scope

| Issue | Component | Title |
|-------|-----------|-------|
| [#3](https://github.com/TheTechChild/pi-remote/issues/3)  | ext         | M3: Pi event projection and forwarding |
| [#10](https://github.com/TheTechChild/pi-remote/issues/10) | daemon      | M3: Coordinator WebSocket client with auth + reconnect |
| [#11](https://github.com/TheTechChild/pi-remote/issues/11) | daemon      | M4: Session registry and event multiplexing |
| [#18](https://github.com/TheTechChild/pi-remote/issues/18) | coordinator | M1: Daemon WebSocket endpoint with CF service-token auth |
| [#19](https://github.com/TheTechChild/pi-remote/issues/19) | coordinator | M2: Client WebSocket endpoint with CF Access JWT auth |

## Out of scope (deferred)

- **Broker, ring buffer, LRU eviction** — coordinator M3/M4 (§§ 18.1-18.4),
  filed as separate milestone issues (see roadmap). No `attach`/`detach`,
  no `replay_unavailable`, no `session_event`/`session_pty` *forwarding to
  clients* in this batch. The coordinator ingests upstream and stores
  `lastSeq` per session, nothing more.
- **`subscribe_machine_list` / `machine_list` push** — also broker work.
  This batch parses `client_hello` and stops.
- **`pty_input` / `attach` / `spawn_session` client→coordinator routing** —
  needs the broker.
- **Daemon `pty_input` / `abort_session` / `spawn_request` consumption** —
  needs the daemon's tmux integration (M2/M5/M6), not in this batch.
- **Tmux integration** (daemon M2, M5, M6) — `tmux_target` remains the
  Batch-1 placeholder `"untmuxed:0.0"`; pty bytes are simulated through
  the manual harness, not produced by tmux.
- **Heartbeat-timeout → `unresponsive`** — [#42](https://github.com/TheTechChild/pi-remote/issues/42).
  Independent of this batch; can ship anytime.
- **Reaper for `ended` entries** — [#43](https://github.com/TheTechChild/pi-remote/issues/43).
  Same.
- **Suspend/resume detection** (daemon M7, § 7.7) — separate milestone.
  This batch *does* wire `machine_suspending` and `session_resume` *frame
  emission* because the coordinator endpoint must accept them on reconnect,
  but the trigger for them is stubbed: emitted manually from a debug
  endpoint or test hook, not from an OS suspend signal.
- **Push, ntfy, encryption** — not in scope.
- **Real Cloudflare middleware** — this batch uses a pluggable
  `auth.Middleware` interface with a stub implementation; real CF Access
  validation lands when the coordinator is actually deployed.
- **ext M5 / M6 / M7** (project resolution, spawn-token correlation,
  graceful disconnect detail work) — independent ext milestones; not
  dependencies for this batch.

## Contract surface

### Schemas touched (read-only — no edits expected)

`pi-remote-spec/protocol/daemon-coordinator/` — all 14 schemas:

```
abort_session.json          machine_register.json       machine_resumed.json
machine_suspending.json     pty_input.json              pty_resize.json
session_ended.json          session_event.json          session_pty.json
session_resume.json         session_started.json        session_state_change.json
spawn_request.json          spawn_response.json
```

`pi-remote-spec/protocol/coordinator-app/` — for M2 we read
`client_hello.json` (plus the other 15 will be generated, even though only
`client_hello` is consumed in this batch).

`pi-remote-spec/protocol/extension-daemon/event.json` — the existing schema
is already what ext M3 projects onto. No edits.

### Regen required

- **`pi-remote-daemon/internal/proto/daemon-coordinator/`** — does not
  exist yet. First task in the daemon workstream is
  `bash pi-remote-daemon/scripts/codegen.sh` (which regenerates *all* legs;
  inspect the diff and commit it).
- **`pi-remote-coordinator/internal/proto/`** — currently a `.gitkeep`.
  First task in the coordinator workstream is
  `bash pi-remote-coordinator/scripts/codegen.sh`.
- **`pi-remote-ext/src/proto/`** — already complete from Batch 1; ext M3
  consumes the existing `extension-daemon/event.ts`. No regen needed.

### Schema-change call-out (D25)

If during implementation we discover a schema gap — e.g., we want a
`session_started.metadata` field that isn't on the schema, or we need
`tmux_target` on `session_started` because the coordinator wants it — that
change **must ship as a single PR touching:**

1. `pi-remote-spec/protocol/.../*.json` (the schema edit)
2. `pi-remote-daemon/internal/proto/daemon-coordinator/*.go` (regen)
3. `pi-remote-coordinator/internal/proto/daemon-coordinator/*.go` (regen)
4. `pi-remote-ext/src/proto/extension-daemon/*.ts` (regen, if the
   extension-daemon leg is touched)
5. Any consumer code adjustments in all three components.

This is the SPEC.md § D25 rule. Plan it explicitly: if anyone on either
workstream hits "I need a new field," they raise it on the issue thread
*before* writing code; we land it as a spec PR first, then the
component PRs rebase on that.

**Expected during this batch:** zero schema edits. Every wire field the
five issues require is already in the schemas.

## Workstream A — `pi-remote-ext` M3 (#3)

**New files:**

```
pi-remote-ext/src/
├── events.ts              # Pi event projector: PiEvent -> Event JSONL frame
└── events-table.ts        # const map of Pi-event-name -> projector function (data-driven)
pi-remote-ext/test/
├── events.test.ts         # per-kind projector unit tests + harness wiring
└── events-table.test.ts   # exhaustiveness check vs. the schema enum
```

**Touch:**

- `pi-remote-ext/src/index.ts` — register the events table against
  `ctx.on?.(...)` for each Pi event. Build on the existing
  `session_start` / `session_shutdown` wiring from Batch 1.

**Test surface:**

| Behavior | Test |
|----------|------|
| `agent_start` → `event { kind: "agent_start", data: {} }` | `events.test.ts` |
| `agent_end` with messages summary → `event { kind: "agent_end", data: { messages_summary: {...} } }` | `events.test.ts` |
| `extension_ui_request` → `event { kind: "attention_dialog", data: { method, title, options } }` | `events.test.ts` |
| `tool_execution_end` with `isError: true` → `event { kind: "tool_failure", data: { toolName, error } }` | `events.test.ts` |
| `tool_execution_end` with `isError: false` → **no** event emitted | `events.test.ts` |
| `queue_update` → `event { kind: "queue_update", data: { pending: N } }` | `events.test.ts` |
| `model_select` → `event { kind: "model_select", data: { model } }` | `events.test.ts` |
| `compaction_start` / `compaction_end` → corresponding `event` kinds | `events.test.ts` |
| `extension_error` → `event { kind: "extension_error", data: { message } }` | `events.test.ts` |
| `ts` is `Date.now()` at projection time (vi.setSystemTime) | `events.test.ts` |
| Every kind in `Event["kind"]` (generated union) has a handler entry | `events-table.test.ts` (exhaustiveness) |
| Frames validate against the generated `Event` type (compile-time + runtime sanity assertion) | `events.test.ts` |
| Projection happens *only* after `registerWithDaemon` resolves; events fired pre-register are dropped with a debug log | `events.test.ts` |
| Events are flushed in order even when emitted synchronously in a burst | `events.test.ts` |

**Key design choices:**

- **Pure projectors.** Each handler is `(piPayload) => Event`. No side
  effects, no daemon-socket reference. The wire-up layer in `index.ts`
  applies the projector and hands the frame to `socket.send(...)`.
- **Data-driven table** with TypeScript `satisfies` against
  `Record<PiEventName, Projector>`. Adding a new Pi event = one line in the
  table. The exhaustiveness test asserts that every value in `Event["kind"]`
  (the generated union literal) has at least one Pi event projecting to it.
- **Pre-register drop policy.** If `session_start` fires events before
  `register_ack` arrives, those events are dropped (logged at debug). The
  ordering invariant the daemon assumes — `register` is the first frame —
  is preserved.
- **No backpressure handling.** `socket.send(...)` is fire-and-forget in
  Batch 1. If we later need queueing during reconnect, that lives in
  `socket.ts`, not here.
- **No exposure of Pi-side types in the wire payload.** Projectors strip
  Pi's internal field names and map only to the schema's `data` shape.

**Acceptance per issue (cross-referenced to #3):**

- Every event in § 6.4 has a projector entry. (table test enforces.)
- Each handler is a pure projector. (no daemon dependency in
  `events.ts`.)
- Uses generated wire types from `src/proto/extension-daemon/event.ts`.
  (type assertion on the projector return.)
- Unit tests cover each kind's payload shape.

## Workstream B — `pi-remote-daemon` M3 + M4 (#10, #11)

These two ship in **one PR**. They share the codegen commit, the
coordinator socket goroutine, and the multiplex channel; splitting them
would force a second round of test scaffolding for the multiplex and leave
M3 with no consumer.

**First commit on the branch:** generated proto.

```sh
bash pi-remote-daemon/scripts/codegen.sh
git add pi-remote-daemon/internal/proto
git commit -m "chore(daemon): generate daemon-coordinator wire types from pi-remote-spec/"
```

**New files:**

```
pi-remote-daemon/internal/
├── coordinator/
│   ├── client.go            # WebSocket dial loop, auth headers, reconnect, send/recv split
│   ├── auth.go              # service-token credential loading (D13/D14 file paths)
│   ├── frames.go            # typed-frame helpers (NewSessionEvent, etc.) wrapping proto types
│   ├── client_test.go       # stub WS server: connect, headers, reconnect, machine_register
│   ├── auth_test.go         # credential file reading, missing/short/bad-mode failure modes
│   └── frames_test.go       # round-trip a session_event through frames.go + the proto type
├── session/
│   ├── multiplex.go         # NewMultiplex(reg, coord) — wires the ext->coord pipeline
│   ├── multiplex_test.go    # event ordering, per-session seq monotonicity, drop-on-disconnect
│   └── seq.go               # SeqAllocator — concurrent-safe monotonic per-session counter
```

**Touch:**

- `pi-remote-daemon/internal/session/session.go` — `SessionID` becomes a
  UUIDv7 string (already a string; just switch the generator on creation).
  Add a `StartedAt` timestamp setter on register. No struct changes.
- `pi-remote-daemon/internal/session/registry.go` — add hooks for the
  multiplex: a `WatchEvents(ctx) <-chan SessionFrame` style channel, or a
  callback registration (`OnRegister`, `OnHeartbeat`, `OnEnded`,
  `OnEvent`). **Decision: callbacks**, not channels. Channels would force
  buffering decisions before we know the multiplex's drop policy. A single
  registered callback per event family is enough; the multiplex *is* the
  consumer.
- `pi-remote-daemon/internal/socket/conn.go` — when an `event` frame
  arrives from the extension, hand it to the multiplex (`reg.OnEvent`)
  before / instead of just logging it. The conn handler keeps writing
  `register_ack`; everything else (`event`, `heartbeat`, `disconnect`)
  routes through the registry, which fires the appropriate multiplex
  callback.
- `pi-remote-daemon/internal/config/config.go` — `CoordinatorConfig`
  already has `URL`, `ServiceTokenIDFile`, `ServiceTokenSecretFile`. Add a
  reconnect-tuning section (or just hardcode 1s → 60s exponential like the
  ext socket).
- `pi-remote-daemon/cmd/pi-remote-daemon/main.go` — start the coordinator
  client alongside the socket listener; both die on context cancel.
- `pi-remote-spec/errors/codes.md` — **no edit needed** for this batch.
  All new error conditions (auth failure on coordinator side, etc.) use
  existing codes (`ERR_COORD_AUTH_REQUIRED`).

**Test surface:**

| Behavior | Test |
|----------|------|
| Coordinator client dials configured URL and sets `CF-Access-Client-Id` + `CF-Access-Client-Secret` headers | `client_test.go` (stub WS server inspects upgrade headers) |
| Missing service-token files → daemon logs error and retries with backoff | `auth_test.go` + `client_test.go` |
| Service-token files exist but mode != 0600 → loaded with a warning, not refused (for ergonomics; D13 spec is aspirational) | `auth_test.go` |
| First frame after WebSocket open is `machine_register` with values from config | `client_test.go` |
| Coordinator drops connection → client reconnects with exponential backoff 1s, 2s, 4s, ..., capped at 60s | `client_test.go` w/ fake clock |
| On reconnect, daemon emits one `session_resume` per still-live registry session | `client_test.go` |
| `session_resume.last_seq_emitted` matches the registry's `LastSeq` for that session | `client_test.go` |
| Per-session `LastSeq` starts at 1 and increases monotonically | `multiplex_test.go` |
| `LastSeq` is per-session — two sessions both start at 1 | `multiplex_test.go` |
| `LastSeq` persists across coordinator disconnect (drop policy: events lost, seq preserved) | `multiplex_test.go` |
| Concurrent `OnEvent` calls under `-race` produce monotonic seq per session | `multiplex_test.go` |
| `register` from ext → multiplex emits `session_started` with correct `metadata` shape | `multiplex_test.go` |
| Heartbeat from ext → multiplex emits **nothing** to coordinator (heartbeats are ext-side only; coordinator infers liveness from frame cadence + WebSocket ping) | `multiplex_test.go` |
| `disconnect` from ext → multiplex emits `session_ended { reason: "session_shutdown" }` | `multiplex_test.go` |
| `MarkEnded` (socket-close without disconnect) → multiplex emits `session_ended { reason: "process_exit" }` | `multiplex_test.go` |
| When coordinator socket is disconnected, multiplex events are dropped (not buffered) but `LastSeq` still advances | `multiplex_test.go` |
| UUIDv7 session IDs (per D17) — sortability spot-check | `session_test.go` or `registry_test.go` |

**Key design choices:**

- **`internal/coordinator` is split into client + auth + frames** so the
  client's reconnect loop is independent of credential loading. Tests for
  reconnect don't need real files on disk; tests for auth don't need a
  fake server.
- **WebSocket library: `github.com/coder/websocket` v1.8.x** per § 22.2.
  Use `websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: ...})`
  for the auth headers, and `wsjson.Read` / `wsjson.Write` for frames
  (we're JSON, not binary).
- **WebSocket framing.** Per § 10 ("JSON-over-WebSocket with one frame per
  message"), every frame is one **text** WebSocket frame. We do not use
  binary; pty bytes ride in `session_pty.bytes` as base64 inside a JSON
  frame. Document this in `client.go` so the future tmux/pty work doesn't
  reach for binary frames out of habit.
- **Ping/pong.** Rely on the library's built-in keepalive (the coder
  library handles this). Don't layer an application ping over the
  daemon-coordinator link; the daemon-side `ping.json` schema is only for
  the ext-daemon link. The coordinator can detect daemon disappearance via
  TCP/WS close + missing message cadence.
- **Close codes.** On clean shutdown the daemon sends close code 1000
  ("normal closure"). On auth failure during reconnect, the server's 403
  upgrade response becomes a `websocket.CloseError` with code
  `StatusPolicyViolation` (1008); the client logs and retries (D23 says
  retry with backoff — never give up).
- **Reconnect identity = `machine_id`.** Every reconnect re-sends
  `machine_register` with the same `machine_id`. The coordinator keys its
  machines registry on `machine_id`, so a stale entry from the previous
  connection is replaced atomically. Document explicitly that the
  coordinator must tolerate a fresh `machine_register` for an already-
  known machine and treat it as a take-over (close the previous socket
  if still bound; this is an edge case that comes up if the daemon
  restarts faster than the previous TCP socket times out).
- **Drop policy when coordinator is offline.** The multiplex sees the
  coordinator client's `connected bool`; when false, it allocates `seq`
  but discards the frame. This satisfies the #11 acceptance "Drop policy
  on coordinator-disconnect: events lost, seq preserved." Document the
  trade-off in `multiplex.go`: clients reattaching will see a seq gap
  >1 and the broker will respond with `replay_unavailable` when those
  events fall outside any ring. Acceptable for v1; disk-buffered fallback
  is v2 per § 7.8.
- **UUIDv7 (D17).** Switch session-ID generation from
  `crypto.randomUUID()` (v4) on the ext side **stays v4 for this batch**
  because the daemon is the authority on coordinator-visible IDs. Daemon-
  side IDs (machine_id-scoped events, request_ids in spawn_request, etc.)
  use `github.com/google/uuid` v1.6+ `uuid.NewV7()`. The ext's
  Pi-process-scoped `session_id` continues to be v4 until the broker work
  (Batch 6) actually needs sortability; the registry tolerates either.

**Acceptance per issue:**

- **#10:**
  - Reads service-token credentials from D13/D14 paths ✓ (`auth.go`)
  - Sets `CF-Access-Client-Id` + `CF-Access-Client-Secret` headers on
    connect ✓ (`client.go`)
  - Exponential backoff 1s → 60s ✓ (`client_test.go`)
  - Sends `machine_register` immediately on connect ✓ (`client.go`)
  - Tests use a stub WS server ✓ (`client_test.go` boots an
    `httptest.Server` that upgrades)
- **#11:**
  - UUIDv7 session IDs (D17) ✓ — daemon-side for any IDs it mints; ext-
    side IDs round-trip as opaque strings, registry accepts both.
  - Per-session monotonic `LastSeq` across process lifetime ✓
    (`SeqAllocator`)
  - Concurrent-safe under `-race` ✓ (`multiplex_test.go`)
  - Drop policy on coordinator-disconnect: events lost, seq preserved ✓
    (`multiplex_test.go`)

## Workstream C — `pi-remote-coordinator` M1 + M2 (#18, #19)

**First commit on the branch:** generated proto.

```sh
bash pi-remote-coordinator/scripts/codegen.sh
git add pi-remote-coordinator/internal/proto
git commit -m "chore(coordinator): generate wire types from pi-remote-spec/"
```

**New files:**

```
pi-remote-coordinator/internal/
├── http/
│   ├── daemon_ws.go         # /v1/daemon WebSocket handler
│   ├── daemon_ws_test.go
│   ├── client_ws.go         # /v1/client WebSocket handler
│   ├── client_ws_test.go
│   └── mux.go               # (existing — extended to register the new routes + auth middleware)
├── auth/
│   ├── middleware.go        # Middleware interface: ServiceToken, AccessJWT
│   ├── stub.go              # in-process stub for tests + local smoke
│   ├── cfaccess.go          # placeholder real implementation (delegates to env in v1)
│   ├── middleware_test.go
│   └── stub_test.go
├── machines/
│   ├── registry.go          # machine_id -> *Machine; wraps the daemon socket goroutine
│   ├── machine.go           # struct: ID, DisplayName, DaemonVersion, Capabilities, Conn, LastSeen
│   ├── ingest.go            # frame dispatch: machine_register, session_started, session_event, ...
│   ├── ingest_test.go
│   └── registry_test.go
├── clients/
│   ├── registry.go          # client_id -> *Client; in-memory for this batch
│   ├── client.go            # struct: ID, DeviceDisplayName, Conn, LastSeen, X25519 pubkey
│   └── registry_test.go
└── sessions/
    ├── registry.go          # session_id -> *Session metadata + LastSeq (no ring buffer yet)
    └── registry_test.go
```

**Touch:**

- `pi-remote-coordinator/internal/http/mux.go` — register the two new
  routes (`/v1/daemon`, `/v1/client`) wrapped in their respective auth
  middleware.
- `pi-remote-coordinator/internal/config/config.go` — already has
  `AccessAud` and `ServiceTokenAudience`. No struct change; reads them.
- `pi-remote-coordinator/cmd/pi-remote-coordinator/main.go` — construct
  the auth middleware (real or stub based on a `-auth=cfaccess|stub` flag
  for now), construct the machines/clients/sessions registries, hand them
  to `NewMux`.

**Test surface:**

| Behavior | Test |
|----------|------|
| `GET /v1/daemon` without service-token headers → 403 + body containing `ERR_COORD_AUTH_REQUIRED` | `daemon_ws_test.go` |
| `GET /v1/daemon` with valid service-token (stub middleware injects identity) → upgrades to WebSocket | `daemon_ws_test.go` |
| First frame must be `machine_register`; any other type → close with 1008 + log | `daemon_ws_test.go` |
| `machine_register` accepted → entry added to machines registry | `daemon_ws_test.go` / `registry_test.go` |
| Second `machine_register` for the same `machine_id` (e.g., daemon restart) → previous socket closed, new entry replaces | `registry_test.go` |
| `session_started` → entry added to sessions registry with `LastSeq=0` | `ingest_test.go` |
| `session_event` with `seq > LastSeq` → `LastSeq` updated, frame logged (no fan-out yet) | `ingest_test.go` |
| `session_event` with `seq <= LastSeq` (out-of-order / replay) → frame logged but `LastSeq` not regressed | `ingest_test.go` |
| `session_pty` → same as `session_event` for `LastSeq` semantics | `ingest_test.go` |
| `session_ended` → session entry marked ended; sessions registry retains it (no reaper yet) | `ingest_test.go` |
| `session_state_change` → session's `State` updated | `ingest_test.go` |
| `machine_suspending` → machine's state flips to `suspended`, all its sessions to `paused` | `ingest_test.go` |
| `session_resume` on a known session → re-registers, restores `LastSeq` from `last_seq_emitted` | `ingest_test.go` |
| Daemon socket closes → machine entry stays (we don't have a state for "machine disconnected" yet; flag in followup); active sessions transition to `paused` | `daemon_ws_test.go` |
| `GET /v1/client` without `CF_Authorization` cookie → 403 + body containing `ERR_COORD_AUTH_REQUIRED` | `client_ws_test.go` |
| `GET /v1/client` with valid stub JWT → upgrades | `client_ws_test.go` |
| `GET /v1/client` with expired/malformed JWT → 403 | `client_ws_test.go` |
| First frame must be `client_hello`; client_id must be known | `client_ws_test.go` |
| `client_hello` with unknown `client_id` → close 1008 + log (`/v1/clients/register` is a later milestone) | `client_ws_test.go` |
| Auth middleware boundary: `auth/stub.go` is the only auth impl used by tests; `auth/cfaccess.go` is wired only in production main | `middleware_test.go` |

**Key design choices:**

- **`auth.Middleware` interface.** Two methods: `ServiceToken(r) (Identity,
  error)` and `AccessJWT(r) (Identity, error)`. Both return a typed
  `Identity` (machine_id for service token, email for JWT) or
  `ErrUnauthenticated`. The middleware decides; the handler does not
  re-implement auth.
- **`auth/stub.go`** reads the headers/cookie and trusts them verbatim if
  they match a hardcoded test fixture (e.g.,
  `CF-Access-Client-Id: test-machine` → `Identity{MachineID: "macbook-pro"}`,
  `Cookie: CF_Authorization=test-jwt-clayton` →
  `Identity{Email: "clayton@example.com", ClientID: "test-client-1"}`).
  This is what tests and the local smoke harness use.
- **`auth/cfaccess.go`** is a placeholder that reads a real CF Access JWT
  using `github.com/cloudflare/cloudflare-go` or the standard `jwt`
  library against the JWKS endpoint. Implementation deferred to when we
  actually deploy; the interface is what matters for this batch. A small
  unit test asserts the constructor accepts the `access_aud` from config
  and that it can be instantiated, but the validation path is exercised
  only via integration once the deployment work starts.
- **WebSocket library on the server side: `github.com/coder/websocket`
  v1.8.x** per § 22.3 — same library both ends. Use
  `websocket.Accept(w, r, &websocket.AcceptOptions{...})`.
- **Origin check.** Phase-1 sets `InsecureSkipVerify: true` on accept
  (because we're behind CF Tunnel in production; the origin check is at
  CF). Add a TODO referencing the CF Tunnel deployment work.
- **No fan-out yet.** `ingest.go` updates the in-memory registries and
  logs each frame; that's it. Adding the per-client fan-out is broker
  work (M3/M4 of coordinator) which has its own issue.
- **Single goroutine per daemon connection** reading frames; ingest is
  synchronous inside that goroutine. We do not yet need a write goroutine
  because we don't send anything back beyond the close frame. (When the
  broker lands, write goroutine joins.)
- **Sessions registry** is a thin in-memory map keyed by `session_id`. The
  `Session` struct here is the *coordinator-side* model from § 8.4 minus
  the `Ring` field (no broker yet) and minus `AttachedClients` (no
  attach yet). When the broker lands it expands the struct in place.

**Acceptance per issue:**

- **#18:**
  - Verifies `CF-Access-Client-Id` against configured
    `service_token_audience` ✓ (via stub for now; real validation is the
    cfaccess impl)
  - Upgrades via `github.com/coder/websocket` ✓
  - Reads `machine_register` and registers in machines registry ✓
  - Rejects unauthenticated upgrades with 403 + `ERR_COORD_AUTH_REQUIRED` ✓
  - Tests use a stub middleware ✓ (`auth/stub.go`)
- **#19:**
  - Validates JWT via configured `access_aud` ✓ (same stub pattern)
  - Accepts `client_hello` and looks up client by `client_id` ✓
  - Tests cover valid / expired / malformed JWTs ✓

## Definition of done — smoke procedure

Local three-terminal smoke. Uses the stub auth middleware on the
coordinator (so we don't need a real CF tunnel). Test fixture credentials:

- Service token: `CF-Access-Client-Id: test-machine`, secret ignored.
- Client JWT: `Cookie: CF_Authorization=test-jwt-clayton`.

```sh
# Terminal A — coordinator
cd pi-remote-coordinator
go run ./cmd/pi-remote-coordinator -auth=stub -listen=:8080

# Terminal B — daemon (with override to use stub coordinator URL)
cd pi-remote-daemon
PI_REMOTE_COORDINATOR_URL=ws://localhost:8080/v1/daemon \
PI_REMOTE_SERVICE_TOKEN_ID_FILE=/tmp/pi-remote-test-id \
PI_REMOTE_SERVICE_TOKEN_SECRET_FILE=/tmp/pi-remote-test-secret \
go run ./cmd/pi-remote-daemon
# (after writing "test-machine" / "test-secret" into those files, mode 0600)

# Terminal C — ext harness with M3 event firing
cd pi-remote-ext
node scripts/manual-ext-harness.mjs
# the harness gains a sub-prompt: type "agent_start", "agent_end", "tool_failure", etc.
# to fire that Pi event into the projector
```

**Coordinator stderr (slog JSON, abridged):**

```
{"level":"INFO","msg":"daemon_ws upgrade","machine_id":"macbook-pro"}
{"level":"INFO","msg":"machine_register accepted","machine_id":"macbook-pro"}
{"level":"INFO","msg":"session_started","session_id":"...","machine_id":"macbook-pro"}
{"level":"DEBUG","msg":"session_event","session_id":"...","seq":1,"kind":"agent_start"}
{"level":"DEBUG","msg":"session_event","session_id":"...","seq":2,"kind":"agent_end"}
{"level":"INFO","msg":"session_ended","session_id":"...","reason":"session_shutdown"}
```

**Reconnect probe** (validates #10's reconnect + drop policy and #18's
take-over):

```sh
# In Terminal A: Ctrl-C the coordinator, wait 5s, restart it.
# In Terminal B: daemon log shows "coordinator disconnected", then "reconnect attempt N", then "reconnected".
# Expected: on reconnect, daemon emits machine_register + one session_resume per still-live session
# Expected coordinator log: "session_resume" with last_seq_emitted matching whatever the daemon emitted last.
```

**Client smoke** (validates #19):

```sh
# Quick wscat probe; install with `npm i -g wscat` if needed.
wscat -c ws://localhost:8080/v1/client \
  -H "Cookie: CF_Authorization=test-jwt-clayton"
# Then send:
> {"type":"client_hello","v":1,"client_id":"test-client-1","app_version":"0.0.1"}
# Expected: connection stays open; coordinator log shows "client_hello accepted"
# (no machine_list reply yet — broker work.)
```

**Auth-failure probes:**

```sh
# No headers → 403, body includes ERR_COORD_AUTH_REQUIRED
curl -i http://localhost:8080/v1/daemon
curl -i http://localhost:8080/v1/client
# Wrong client_id in client_hello → close frame 1008
wscat -c ws://localhost:8080/v1/client -H "Cookie: CF_Authorization=test-jwt-clayton"
> {"type":"client_hello","v":1,"client_id":"not-real","app_version":"0.0.1"}
```

**Race smoke:**

```sh
cd pi-remote-daemon && go test -race ./...
cd pi-remote-coordinator && go test -race ./...
cd pi-remote-ext && yarn vitest run
```

All green → batch done.

### Faking CF locally — how the stubs work

- Coordinator: `auth/stub.go` is selected via the `-auth=stub` flag. It
  reads `CF-Access-Client-Id` and accepts any value, mapping the value
  directly to a synthetic machine_id. For `/v1/client`, it reads the
  `CF_Authorization` cookie; the value `test-jwt-clayton` maps to a
  pre-seeded `client_id` in the clients registry. Anything else → 403.
- Daemon: the credential files contain plain strings; the daemon reads
  them and stuffs them into the two headers. Stub coordinator accepts
  them.
- Client (wscat / Android dev build): same — set the cookie to the stub
  value.

No real CF Tunnel needed for this batch. Real validation comes online
during the deployment milestone.

## Parallelization map

This batch **cannot** parallelize as cleanly as Batch 1 because Workstream
B and Workstream C *converge on the daemon-coordinator wire protocol*.
The schemas are already locked, so they don't need to land together — but
their behavior is symmetric (one writes a frame the other reads), and
testing each side in isolation is only convincing once both sides agree.

### What can run in worktrees in parallel

- **Workstream A** (`ext/m3-event-projection`) is **fully independent**.
  It depends only on the existing `extension-daemon/event.json` schema and
  the existing socket. Land it independently, in its own PR. Other
  workstreams do not need to wait.
- **Workstream B's codegen commit** can land in a tiny standalone PR
  (`chore(daemon): regenerate proto`) ahead of the M3+M4 work, to make the
  feature PR's diff smaller. Same for Workstream C's codegen.

### What must converge

- **Workstreams B (daemon M3) and C (coordinator M1) share the WebSocket
  framing semantics.** They can be developed in parallel worktrees, but
  the integration smoke test (above) is what proves they agree. Plan to
  do a final convergence pass once both PRs are individually green:
  rebase, merge B first (it has nothing pointed at it), merge C second,
  then run the smoke test on `main`. If the smoke test fails it's
  framing-disagreement; iterate with a follow-up PR.
- **Workstream B and Workstream C may both want to add the same field
  somewhere** in the middle of implementation. Per D25 that is a
  cross-component change: stop both worktrees, file a spec PR, regen
  everywhere, then rebase. Do not bypass.

### Worktree layout (suggested)

```sh
cd ~/projects/pi-remote

# Workstream A
git worktree add ../pi-remote-ext-m3 -b ext/m3-event-projection main

# Workstream B
git worktree add ../pi-remote-daemon-m3m4 -b daemon/m3-m4-coordinator main

# Workstream C
git worktree add ../pi-remote-coord-m1m2 -b coordinator/m1-m2-ws-endpoints main
```

### What you cannot put in separate worktrees this batch

- **Any schema edit.** If discovered, it's a single PR touching the spec
  and all three components. Stop the worktrees, do the spec PR on `main`,
  resume.

## Risks and gotchas

### Auth header injection

- **Daemon side:** the WebSocket upgrade request is just an HTTP request
  with extra headers. With `coder/websocket`'s `DialOptions.HTTPHeader`,
  the headers go onto the upgrade request, not subsequent frames (there
  are no per-frame headers in WS). Read the credentials *once at dial
  time*; if they change, you reconnect to re-read. Don't try to hot-swap.
- **Coordinator side:** the headers and cookie arrive on the upgrade
  `*http.Request`, before `websocket.Accept`. Validate them **first**,
  then call `Accept`. If you Accept first and validate after, you've
  already 101'd the client; you can only close, not 403.

### WebSocket framing — text vs binary, ping/pong, close codes

- **Text frames only.** Every message is JSON. Pty bytes are base64 inside
  JSON (`session_pty.bytes`). This is wasteful but is what the schemas
  specify; v2 may switch to binary frames for `session_pty`.
- **Ping/pong:** library-managed. Don't add an application-level keepalive
  on the daemon-coordinator link. (The ext-daemon link's `heartbeat` is a
  Pi-process liveness signal, not a transport keepalive — different
  layer.)
- **Close codes:**
  - 1000 normal closure: clean shutdown.
  - 1001 going away: machine_suspending.
  - 1008 policy violation: auth fail, wrong first frame, bad JSON.
  - 1011 internal error: panic recovery.
  - 1012 service restart: coordinator graceful restart (post-batch
    nicety).

### Reconnect identity (`machine_id`)

- `machine_id` is config — `machine_id = "macbook-pro"` from § 7.3. It
  does **not** change across reconnects. The coordinator must treat a
  second `machine_register` with the same `machine_id` as a take-over,
  not a duplicate-rejected. Tests must cover this explicitly.
- If the daemon restarts and loses `LastSeq` (process kill, no persistence
  yet), D18 says start at `last_known + 1000` to avoid coordinator-side
  ring confusion. **This batch doesn't persist `LastSeq` to disk.** We
  start at 1 on every daemon process boot, accepting the D18 violation;
  filed as a future hardening. (Note this in the daemon README.) The
  broker work (Batch 6) will revisit.

### Per-session `seq` numbers

- One `SeqAllocator` per session, not one global. Two sessions on the same
  daemon both start at 1; this is correct per § 18.2.
- `seq` increments **before** the frame is written, even if write fails
  (drop-on-disconnect policy). That way the coordinator-side gap detection
  matches reality: "events 5 and 6 happened, you'll never see them, here
  comes event 7."
- The allocator is per-session, not per-message-type: `session_event`
  seq=1, `session_pty` seq=2, `session_event` seq=3 is fine and matches
  § 18.2.

### Drop policy on coordinator disconnect

- Per #11 and § 7.8: events lost, seq preserved. **Do not buffer in
  memory** — that defers the question to OOM. Do not buffer on disk —
  that's v2.
- The multiplex's "I'm currently connected" signal comes from the
  coordinator client (`coord.Connected() bool`). When false, the multiplex
  allocates a seq and discards. When true, it writes. Race condition
  between "checked Connected, was true, wrote, write failed because
  coordinator just dropped": fine, treat as drop, log at debug.

### CF Access middleware boundary

- Keep `cfaccess.go` thin and untested in this batch. The contract is the
  `Middleware` interface. Tests use `stub.go`. When we deploy, we'll
  swap the real impl in and add integration tests that hit a real CF
  Access endpoint with real credentials — that work is its own PR.

### Event ordering through the ext

- The ext's events.ts handlers run synchronously in response to Pi events.
  Per § 6.4, Pi guarantees event order; we must not reorder. Don't queue
  events through a `setImmediate` or `setTimeout(0)`; the projector
  hands the frame to `socket.send` directly.

### Heartbeat-timeout (#42) and reaper (#43)

- These are deferred but **may interact** with multiplex. Specifically,
  when #42 lands, the `unresponsive` state change must emit a
  `session_state_change` frame upstream. That's noted in #42's acceptance.
  This batch should leave the multiplex's `OnStateChange` callback hook
  in place even though nothing fires it yet, so #42 has somewhere to
  plug in.
- Reaper #43 needs the daemon to retain `EndedAt`. Add the field to the
  `Session` struct as part of this batch's "while we're touching
  session.go" pass; it's not used here but it costs nothing and unblocks
  the reaper PR. (Strictly optional; #43 can also add it itself.)

### Coordinator socket idle handling

- A daemon may go quiet for minutes (idle session, no Pi events, no pty
  output). The library's ping keepalive prevents this from being
  mistaken for a dead connection. Verify with the `coder/websocket`
  config — if its default ping interval is too long for our taste, set
  one explicitly (suggest 30s).

## Suggested PR rhythm

Three PRs, landed in this order:

1. **`ext/m3-event-projection`** (#3) — independent. Closes #3. Workstream
   A, single PR.
2. **`daemon/m3-m4-coordinator`** (#10 + #11) — depends on no one. The
   first commit is the codegen output (separate commit, not separate PR,
   to keep traceability with the feature work). Closes #10 and #11.
3. **`coordinator/m1-m2-ws-endpoints`** (#18 + #19) — depends on no one
   in code, but the smoke test in DOD only goes green once Workstream B
   has merged. Order matters for the on-`main` smoke test, not for
   review. Closes #18 and #19.

After the third merges: run the smoke procedure from a fresh checkout. If
green, batch is done.

**No integration-test PR.** As in Batch 1, the smoke is a local
procedure. A committed end-to-end harness (probably under
`pi-remote-spec/scenarios/` with executable companions) is its own piece
of work and lands when we have the broker — then there's something
meaningful to assert end-to-end.

**Cleanup after merge:**

```sh
cd ~/projects/pi-remote
git worktree remove ../pi-remote-ext-m3
git worktree remove ../pi-remote-daemon-m3m4
git worktree remove ../pi-remote-coord-m1m2
```

## After this batch

Batch 3 (tentative) becomes the broker:

- coordinator M3 — ring buffer + LRU
- coordinator M4 — fan-out + `attach`/`detach`/`replay_unavailable`
- daemon M5 — pty multiplexing (real bytes, not simulated)
- ext M5 — project name resolution (`.pi-remote.toml` + git-root)

That batch can re-parallelize cleanly because the daemon-coordinator wire
will be stable from this batch. The next *unstable* surface is
coordinator-app, which gets serious in Batch 3.
