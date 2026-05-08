# Pi Remote — Technical Specification (v1)

**Status:** v1.1 — finalized for agent-driven implementation
**Target:** v1 ship
**Audience:** the implementer (you, or an AI agent on your behalf)

---

## Abstract

Pi Remote is a session-sharing system for the [Pi](https://pi.dev) coding agent. It lets any of your coding machines publish their live Pi sessions to a coordinator service running on your UnraidOS box, and lets any of your Android devices attach to those sessions to monitor progress, receive push notifications when an agent needs input, and interact bidirectionally with the running TUI.

The architecture preserves full extension fidelity by mirroring the live pty bytes of each Pi process rather than re-rendering structured events. A small Pi extension on each coding machine forwards a side-channel of structured "needs-attention" events to the local daemon, which is what drives push notifications. The mirror approach means every Pi extension you install — including those using `ctx.ui.custom()`, custom editors, custom message renderers, footer/widget replacements — works on the phone exactly as it works locally, and stays working as Pi evolves.

The system has four parts:

1. **Pi extension** (`pi-remote-ext`). TypeScript, distributed as a Pi git package. Registers each Pi session with the local daemon and forwards structured event signals.
2. **Daemon** (`pi-remote-daemon`). Go, runs on each coding machine. Manages tmux-hosted Pi sessions, mirrors their ptys, multiplexes structured events, dials out to the coordinator over Cloudflare Tunnel.
3. **Coordinator** (`pi-remote-coordinator`). Go, runs on UnraidOS in Docker. Brokers attached clients, maintains an in-memory replay ring per session, dispatches push notifications via ntfy.
4. **Android app** (`pi-remote-android`). Native Kotlin. Renders pty streams using Termux's terminal-emulator core. Receives push notifications via UnifiedPush against a self-hosted ntfy distributor.

Auth: Cloudflare Access service tokens for machines, Cloudflare Access email-PIN for clients. Push: UnifiedPush via ntfy with payloads end-to-end encrypted using NaCl `crypto_box` so neither ntfy nor Cloudflare can read them. Live session traffic is TLS-only in v1; application-layer E2EE is a v2 follow-up.

---

## 1. Goals

| # | Goal |
|---|------|
| G1 | Frictionless walk-away workflow: fire off Pi tasks at the keyboard, walk away with phone, get a push notification when the agent needs input, respond from the phone, and resume on the laptop later in the same session. |
| G2 | Full extension fidelity. Every Pi extension you have installed locally must work identically when viewed remotely, with no per-extension porting work. |
| G3 | Multi-machine, multi-device. Both desktop and MacBook publish sessions to the same coordinator; any phone or tablet can attach to any session on any machine. |
| G4 | Always-on share. Any Pi process started anywhere on a coding machine is automatically attachable; no per-session opt-in. |
| G5 | Suspend-aware. When the MacBook lid closes, in-flight sessions pause cleanly. Attached devices show a paused/disconnected state. Sessions resume when the lid reopens. |
| G6 | Self-hosted push. Push notifications do not depend on Google or any third-party push service. The wake-up rail is a self-hosted ntfy server reached via UnifiedPush. |
| G7 | Encrypted push payloads. ntfy and any in-flight transport see only ciphertext. |

## 2. Non-goals (v1)

| # | Non-goal | Reason |
|---|---------|--------|
| N1 | Application-layer E2EE for live session traffic | Real cost (~1500–2500 LOC for Noise + ratchet + multi-attach state). CF terminating TLS is the only exposure and CF's track record makes this an acceptable v1 risk. Layer B in v2. |
| N2 | Image attachment from phone to Pi prompts | Requires structured remote-inject channel beyond pty bytes. Reserved in protocol, implemented in v2. |
| N3 | Tap-to-respond from lock-screen for `confirm` dialogs | Requires structured extension UI sub-protocol bridge. Reserved, implemented in v3. |
| N4 | iOS support | Android-only by user preference. |
| N5 | Multi-user / multi-tenant | Personal use. Whitelist is the union of one user's identities. |
| N6 | LAN shortcut for low-latency local-network access | All traffic flows through CF Tunnel in v1. LAN shortcut is a v2 optimization. |
| N7 | Cross-machine extension/skill sync | Each coding machine has its own `~/.pi/` and that's intentional. |
| N8 | Persisting broker history across coordinator restart | In-memory ring only. Source of truth for full history is Pi's own JSONL on the coding machine. |
| N9 | Wake the MacBook from network when it's suspended | Sessions are read-only-from-app while machine is suspended. No wake-on-LAN logic. |
| N10 | Collaborative multi-user concurrency control | Single-user, all clients have full input rights, last-write-wins is fine. |

## 3. Locked architecture decisions

These were settled during design grilling and are inputs to this spec, not subjects of further debate:

- **Mirror-based architecture (H1)** rather than RPC re-rendering. Pi runs interactively (in tmux) and the mirror is read alongside.
- **tmux for pty management.** Specifically, tmux **control mode** (`tmux -CC`) is the daemon's interface for spawning, attaching, and reading pane output.
- **Go for the daemon and coordinator.** No need to mirror Pi's internal TS types into Go because the wire format is defined in the extension; daemon and coordinator only handle Pi Remote's own schema.
- **Native Kotlin for Android** with Termux's `terminal-emulator` and `terminal-view` modules (Apache 2.0, vendored from termux-app at a pinned commit).
- **Cloudflare Access** for auth: service tokens for daemons, email-PIN + Keystore-stored JWT for clients.
- **Cloudflare Tunnel** for transport on both legs (machine → unraid, phone → unraid).
- **UnifiedPush + self-hosted ntfy** for push wake-up. ntfy app is the distributor on phone.
- **NaCl `crypto_box` (X25519 + XSalsa20-Poly1305)** for push payload E2EE.
- **In-memory ring per session, 50MB total cap, LRU eviction across sessions.**
- **Project model:** project name = immediate parent of cwd; override via `.pi-remote.toml` at cwd or git root.
- **Multi-attach:** all clients full input rights, character-level interleaving accepted as user-managed.
- **MacBook suspend = read-only from phone**, no input queueing while suspended.
- **Always-on share:** any Pi instance with the extension installed self-registers with the daemon; no `/share` step required.
- **Spawn-from-phone supported in v1.** Phone can request a new tmux + Pi session at a specified cwd.
- **Image paste:** v2. **Tap-to-respond:** v3. Reserved in protocol from v1.

---

## 4. System architecture

```
                                  ┌────────────────────────────────────┐
                                  │              Phone(s)              │
                                  │  ┌──────────────────────────────┐  │
                                  │  │  pi-remote-android (Kotlin)  │  │
                                  │  │  - Termux terminal core      │  │
                                  │  │  - WebSocket to coordinator  │  │
                                  │  │  - UnifiedPush receiver      │  │
                                  │  └──────────────────────────────┘  │
                                  │  ┌──────────────────────────────┐  │
                                  │  │  ntfy Android app            │  │
                                  │  │  (UnifiedPush distributor)   │  │
                                  │  └──────────────────────────────┘  │
                                  └────────────────────────────────────┘
                                                    │   ▲
                                  WebSocket (TLS)   │   │ UnifiedPush
                                  CF Access JWT     │   │ wake-up
                                                    ▼   │
                              ┌─────────────────────────────────────┐
                              │       Cloudflare Edge / Access      │
                              │  - Email-PIN auth for clients       │
                              │  - Service-token auth for daemons   │
                              └─────────────────────────────────────┘
                                                    │
                              cloudflared tunnel (outbound from unraid)
                                                    ▼
            ┌─────────────────────────────────────────────────────────────────┐
            │                       UnraidOS box                              │
            │  ┌──────────────────────────────┐  ┌────────────────────────┐   │
            │  │   pi-remote-coordinator      │  │   ntfy server          │   │
            │  │   (Go, Docker)               │──▶  (Docker)              │   │
            │  │   - WebSocket multiplex      │  │   - UnifiedPush API    │   │
            │  │   - Per-session ring (50MB)  │  │   - Self-hosted        │   │
            │  │   - LRU eviction             │  │                        │   │
            │  │   - Push payload encrypt     │  │                        │   │
            │  └──────────────────────────────┘  └────────────────────────┘   │
            └─────────────────────────────────────────────────────────────────┘
                          ▲                ▲
                          │ WebSocket      │ WebSocket
                          │ CF Service     │ CF Service
                          │ Token          │ Token
                          │                │
       cloudflared tunnel │                │ cloudflared tunnel
       (outbound)         │                │ (outbound)
                          │                │
            ┌─────────────────────┐     ┌─────────────────────┐
            │ Desktop coding box  │     │ MacBook Pro         │
            │ ┌─────────────────┐ │     │ ┌─────────────────┐ │
            │ │ pi-remote-      │ │     │ │ pi-remote-      │ │
            │ │ daemon (Go)     │ │     │ │ daemon (Go)     │ │
            │ │ - tmux -CC ctrl │ │     │ │ - tmux -CC ctrl │ │
            │ │ - Unix socket   │ │     │ │ - Unix socket   │ │
            │ └─────────────────┘ │     │ └─────────────────┘ │
            │         ▲           │     │         ▲           │
            │         │ Unix      │     │         │ Unix      │
            │         │ socket    │     │         │ socket    │
            │ ┌───────┴─────────┐ │     │ ┌───────┴─────────┐ │
            │ │ tmux session(s) │ │     │ │ tmux session(s) │ │
            │ │ ┌─────────────┐ │ │     │ │ ┌─────────────┐ │ │
            │ │ │ pi process  │ │ │     │ │ │ pi process  │ │ │
            │ │ │ + extension │ │ │     │ │ │ + extension │ │ │
            │ │ └─────────────┘ │ │     │ │ └─────────────┘ │ │
            │ └─────────────────┘ │     │ └─────────────────┘ │
            └─────────────────────┘     └─────────────────────┘
```

### Trust boundaries

| Boundary | Crossed by | Protection |
|----------|-----------|------------|
| Coding machine ↔ CF edge | cloudflared tunnel | TLS, terminated at CF edge |
| CF edge ↔ unraid | cloudflared tunnel | TLS, terminated at CF edge |
| unraid (coordinator ↔ ntfy) | local Docker network | Docker network isolation |
| ntfy ↔ phone | HTTPS via cloudflared | TLS + payload encrypted with `crypto_box` |
| Coding machine internal (extension ↔ daemon) | Unix socket | Filesystem permissions on socket |
| Phone ↔ CF edge | HTTPS | TLS |

CF can decrypt traffic at its edge for the live session legs in v1. Push payloads are protected from CF by `crypto_box` even if CF could see ciphertext.

---

## 5. Repository layout

Single GitHub user (`TheTechChild`), one repo per component. Pi Remote does not need to be a monorepo — the extension ships as a Pi package, the daemon and coordinator ship as Go binaries, the app is its own Android Studio project.

```
github.com/TheTechChild/
├── pi-remote-ext/           # TS Pi extension, installable via `pi install git:...`
├── pi-remote-daemon/        # Go, runs on each coding machine
├── pi-remote-coordinator/   # Go, runs on unraid in Docker
├── pi-remote-android/       # Kotlin Android Studio project
└── pi-remote-spec/          # This spec, design docs, protocol schemas, ADRs
```

Wire protocol message schemas live in `pi-remote-spec/protocol/` as JSON Schema files. The extension and the Go components consume them via codegen. The Android app reads them as ground truth and consumes them via Kotlin codegen.

---

## 6. Pi extension (`pi-remote-ext`)

### 6.1 Responsibilities

- Auto-load on every Pi session via `~/.pi/agent/extensions/pi-remote-ext/` (set up by `pi install`).
- Connect to the local daemon's Unix socket on `session_start`.
- Register itself with session metadata (cwd, project name, pid, hostname, model, tmux target).
- Forward structured event signals that the daemon needs for notifications and routing.
- Maintain a heartbeat to the daemon.
- Cleanly disconnect on `session_shutdown`.
- **Not** mirror pty bytes. The daemon does that directly via tmux control mode.
- **Not** render footer widgets, dialogs, or any user-visible TUI. Local TUI is unchanged from stock Pi.

### 6.2 Files

```
pi-remote-ext/
├── package.json             # Pi package manifest with "pi": { "extensions": ["./src/index.ts"] }
├── README.md
├── src/
│   ├── index.ts             # Default-export factory function
│   ├── socket.ts            # Unix socket client with reconnect
│   ├── events.ts            # Pi event → daemon message projection
│   ├── project.ts           # Project name resolution (cwd parent, .pi-remote.toml)
│   └── types.ts             # Shared protocol types (codegen target)
└── tsconfig.json
```

`package.json`:

```json
{
  "name": "@thetechchild/pi-remote-ext",
  "version": "1.0.0",
  "type": "module",
  "dependencies": {
    "@iarna/toml": "^3.0.0"
  },
  "pi": {
    "extensions": ["./src/index.ts"]
  }
}
```

### 6.3 Connection lifecycle

```
session_start
   │
   ▼
read PI_REMOTE_SOCKET (env, default ~/.pi-remote/daemon.sock)
   │
   ▼
attempt connect (5s timeout, exponential backoff up to 30s)
   │
   ├── success: send `register`, await `register_ack`
   │             │
   │             ▼
   │      heartbeat loop (every 10s)
   │             │
   │             ▼
   │      forward events as they fire
   │
   └── failure (no daemon running): log, continue without remote.
                                    Pi session works locally with no impact.
```

If the daemon socket goes away mid-session (daemon restart), the extension reconnects with the same `sessionId` and re-registers. The daemon recognizes the existing session and resumes the relationship without re-spawning anything.

### 6.4 Events the extension forwards

The extension hooks the following Pi events and projects them to the daemon's protocol:

| Pi event | Forwarded as | Notes |
|----------|--------------|-------|
| `session_start` | `register` | Initial handshake. |
| `agent_start` | `event` kind `agent_start` | Coordinator uses this to set "running" state. |
| `agent_end` | `event` kind `agent_end` | **Triggers push if no pending queue.** Carries `messages[]` summary. |
| `extension_ui_request` (RPC sub-protocol or custom dialog open) | `event` kind `attention_dialog` | **Always triggers push.** Includes dialog method, title, options. |
| `tool_execution_end` with `isError: true` | `event` kind `tool_failure` | **Triggers push.** Includes `toolName`, error excerpt. |
| `queue_update` | `event` kind `queue_update` | Pending counts. Does not trigger push by default; user-configurable. |
| `model_select` | `event` kind `model_select` | Updates session metadata for client display. |
| `compaction_start` / `compaction_end` | `event` kind `compaction_*` | Informational. |
| `extension_error` | `event` kind `extension_error` | Logged on coordinator; user-configurable for push. |
| `session_shutdown` | `disconnect` | Daemon marks session ended. |

Hook implementation lives in `events.ts`. Each handler is a pure projector: take Pi's event payload, produce a Pi Remote protocol message, forward via `socket.ts`. No business logic in the extension beyond projection.

### 6.5 Project name resolution

On `session_start`, the extension computes the project name as follows:

1. Walk up from `cwd` to find a `.pi-remote.toml`. If found and it contains `project = "..."`, use that.
2. Walk up from `cwd` to find the git root (the directory containing `.git/`). If found, the project name is the basename of the git root.
3. Otherwise, the project name is the basename of the immediate parent directory of `cwd`.

The order is deliberate: explicit override > git-root convention > simple parent default. Git-root is included because it gives sensible behavior for repos with deep cwd structures (e.g., `~/angel-studios/content-collections/packages/api` would group under `content-collections`, not under `packages`).

`.pi-remote.toml` schema:

```toml
project = "content-collections"      # required if file present
display_name = "Content Collections" # optional, prettier name for UI
```

### 6.6 Heartbeat & disconnect

Heartbeats are JSON one-liners every 10 seconds with `{ "type": "heartbeat", "ts": <unix_ms> }`. The daemon expects one within 30 seconds; missing three in a row marks the session as `unresponsive` (different from `paused`). On Pi process exit, the extension's `session_shutdown` handler sends an explicit `{ "type": "disconnect" }` before the socket closes so the daemon doesn't have to wait for heartbeat timeout.

### 6.7 Spawn-token correlation

When the daemon spawns a Pi session itself (in response to a phone request), it sets `PI_REMOTE_SPAWN_TOKEN=<random>` in the environment of the spawned process. The extension's `register` message includes this token verbatim if present, allowing the daemon to correlate the new extension instance with the spawn request it just performed. Pi sessions started by the user without daemon involvement have no spawn token, and the daemon treats them as user-initiated.

---

## 7. Daemon (`pi-remote-daemon`)

### 7.1 Responsibilities

- Listen on a Unix socket for extension registrations.
- Maintain a tmux server connection in control mode (`tmux -CC`).
- Mirror pty output of every tmux pane that has a Pi session registered.
- Spawn new tmux sessions on request from the coordinator.
- Multiplex all session events (structured + pty bytes) to the coordinator over a single WebSocket.
- Authenticate to the coordinator with a Cloudflare Access service token.
- Handle reconnect on transient network failures.
- Detect machine suspend / resume and notify the coordinator.

### 7.2 Files (Go module)

```
pi-remote-daemon/
├── go.mod
├── cmd/pi-remote-daemon/
│   └── main.go              # entrypoint, flag parsing, signal handling
├── internal/
│   ├── socket/              # Unix socket server for extensions
│   ├── tmux/                # tmux -CC control-mode client
│   ├── coordinator/         # WebSocket client, reconnect, auth
│   ├── session/             # session registry, lifecycle, correlation
│   ├── ptymux/              # pty multiplexing, sequence numbers
│   ├── suspend/             # OS-specific suspend/resume detection
│   └── config/              # config file parsing, env loading
├── docs/
│   ├── install-macos.md     # launchd plist template, install steps
│   └── install-linux.md     # systemd unit template
└── README.md
```

### 7.3 Configuration

`/etc/pi-remote/daemon.toml` (system-wide) or `~/.config/pi-remote/daemon.toml` (per-user):

```toml
machine_id = "macbook-pro"          # stable identifier per machine; first run generates a UUID if missing
machine_display_name = "MacBook Pro"

[coordinator]
url = "https://pi-remote.example.com"
service_token_id_file = "/etc/pi-remote/service_token_id"
service_token_secret_file = "/etc/pi-remote/service_token_secret"

[socket]
path = "~/.pi-remote/daemon.sock"

[tmux]
binary = "tmux"
session_prefix = "pi-remote-"

[logging]
level = "info"
file = "~/.pi-remote/daemon.log"
```

Service token credentials live in 0600-mode files outside the config so the config can be checked into dotfiles without secrets.

### 7.4 Process model

The daemon runs as the user, not as root. It does not need elevated privileges.

- **macOS:** `launchd` user agent, plist installed at `~/Library/LaunchAgents/dev.pi-remote.daemon.plist`. Auto-start on login, KeepAlive=true.
- **Linux:** `systemd --user` unit, installed at `~/.config/systemd/user/pi-remote-daemon.service`. `WantedBy=default.target` so it starts on user session.

Single instance per user. Second invocation detects the running socket and exits with an error message pointing at logs.

### 7.5 Daemon-side session model

A daemon-side `Session` is the join of:

- One tmux pane (identified by `session:window.pane` target string).
- One Pi process running in that pane (identified by pid).
- One extension instance (identified by Unix socket connection).

The daemon maintains a registry: `sessionId → Session struct`, where `sessionId` is Pi's own session UUID (taken from the extension's `register` message). State per session:

```go
type Session struct {
    SessionID         string
    SpawnToken        string             // empty if user-spawned
    TmuxTarget        string             // "pi-remote-abc:0.0"
    PID               int
    CWD               string
    ProjectName       string
    Hostname          string
    Model             string
    StartedAt         time.Time
    LastHeartbeat     time.Time
    LastSeq           uint64             // monotonic per-session sequence
    State             SessionState       // running | idle | paused | unresponsive | ended
    AttachedClients   map[string]bool    // client IDs currently attached
}
```

`LastSeq` is used to assign sequence numbers to outgoing events so the coordinator's broker can replay from a known point.

### 7.6 tmux control-mode integration

On startup, the daemon launches a single long-lived tmux client in control mode:

```bash
tmux -CC new-session -d -s pi-remote-control
```

Or attaches to an existing one if the daemon is restarting. Control mode messages stream in over stdout, commands go to stdin. The daemon parses the `%output`, `%window-add`, `%session-changed`, `%pane-mode-changed`, `%exit`, etc. notifications.

For pty mirroring, the daemon uses `%output` notifications which arrive as `%output <pane> <data>` for every byte written to a pane. The daemon de-escapes the wire-format encoding (tmux escapes some control bytes) and forwards the raw bytes.

For spawning new sessions on phone request, the daemon issues:

```bash
new-session -d -s pi-remote-<uuid> -c <cwd> "env PI_REMOTE_SPAWN_TOKEN=<token> pi"
```

The daemon does **not** handle local user attachment to these tmux sessions. The user attaches with `tmux -L default a -t pi-remote-<uuid>` from a regular terminal and works as normal. The daemon's mirror is a passive observer.

### 7.7 Suspend / resume detection

- **macOS:** subscribe to `NSWorkspaceWillSleepNotification` and `NSWorkspaceDidWakeNotification` via cgo or invoke the `pmset` event loop.
- **Linux:** listen on `org.freedesktop.login1` `PrepareForSleep` D-Bus signal.

On suspend, the daemon sends `{ "type": "machine_suspending" }` to the coordinator and closes its WebSocket gracefully. On resume, it reconnects and sends `{ "type": "machine_resumed" }`. Sessions that were running through suspend are still running (the OS paused the processes); on resume the extension and tmux mirror pick back up where they left off.

The daemon does not need to do anything to "pause" Pi — Pi is paused by the OS, not by the daemon.

### 7.8 Reconnect strategy

Coordinator WebSocket reconnect: exponential backoff starting at 1s, capped at 60s. On reconnect, daemon re-announces all live sessions (`machine_register` followed by `session_resume` for each) and includes `lastSeq` per session so the coordinator can assess any backfill it has cached.

The daemon does not buffer events on its own side during disconnection — events the coordinator missed are gone. Per-session sequence numbers allow the coordinator's clients to detect gaps but not recover the missed events from the daemon. (Future work: optional disk-buffered fallback. v2.)

---

## 8. Coordinator (`pi-remote-coordinator`)

### 8.1 Responsibilities

- Accept inbound WebSockets from daemons (CF Service Token-authenticated).
- Accept inbound WebSockets from clients (CF Access JWT-authenticated).
- Broker session events: route from daemon to all attached clients.
- Maintain a per-session in-memory ring buffer for replay on client reconnect.
- Enforce 50MB total cache cap with LRU eviction across sessions.
- Encrypt and dispatch push notifications via ntfy.
- Track machine and session state and propagate state changes to clients.
- Persist nothing to disk in v1.

### 8.2 Files (Go module)

```
pi-remote-coordinator/
├── go.mod
├── cmd/pi-remote-coordinator/
│   └── main.go
├── internal/
│   ├── http/                # WebSocket upgrade endpoints, CF Access middleware
│   ├── machines/            # daemon connection registry
│   ├── clients/             # client connection registry, push key storage
│   ├── sessions/            # session metadata & state
│   ├── broker/              # routing, ring buffer, LRU eviction
│   ├── push/                # ntfy client, NaCl encryption
│   └── config/
├── deploy/
│   ├── docker-compose.yaml  # for unraid
│   └── Dockerfile
└── README.md
```

### 8.3 Configuration

`/config/coordinator.toml`:

```toml
[server]
listen = ":8080"

[cloudflare]
access_aud = "..."                    # CF Access AUD tag for client auth
service_token_audience = "..."        # for daemon auth

[ntfy]
url = "http://ntfy:80"                # internal Docker network name
auth_token = "..."                    # ntfy bearer auth (if configured)

[broker]
total_cache_bytes = 52428800          # 50MB
session_cache_floor_bytes = 1048576   # 1MB minimum per active session before LRU evicts further

[push]
coordinator_keypair_path = "/data/coordinator-keypair.box"  # generated on first run
```

### 8.4 Coordinator-side session model

```go
type Session struct {
    SessionID       string
    MachineID       string
    Metadata        SessionMetadata    // cwd, project, model, hostname, started_at
    State           SessionState       // running | idle | paused | unresponsive | ended
    LastSeq         uint64
    AttachedClients map[string]*Client
    Ring            *RingBuffer        // see § 8.6
    LastTouched     time.Time          // for LRU
}
```

### 8.5 Per-session ring buffer

Each active session has a ring buffer holding the most recent N events and pty chunks. Each entry is a `(seq, kind, payload)` tuple. The buffer is bytes-bounded, not entries-bounded.

Two stream types share the buffer:

- **Structured events** (small, frequent): `agent_start`, `agent_end`, `attention_dialog`, etc. Carry their full payload.
- **Pty chunks** (large, very frequent): raw bytes from tmux `%output`. Variable size, typically 1–4KB per event under heavy output.

Both are sequence-numbered from the same monotonic counter (`LastSeq` on the session) so a client's `lastSeq` value uniquely identifies the resume point regardless of whether the next entry is structured or pty.

### 8.6 LRU eviction across sessions

The total cache footprint across all sessions is capped at `total_cache_bytes` (50MB). When a new event needs to be appended and the total cache size would exceed the cap:

1. Identify the session with the **oldest `LastTouched`** time among sessions that are currently above their `session_cache_floor_bytes`.
2. Evict the **oldest entries from that session's ring** until either the global cap is satisfied or the session is at its floor.
3. If still over cap, repeat with the next-oldest-touched session.
4. If all sessions are at floor and still over cap (this would require a lot of active sessions), evict from the most-recently-touched session that isn't currently being read by a client.

`LastTouched` is updated on any append or read. The floor (`session_cache_floor_bytes`, 1MB) ensures a quiet session retains *some* history rather than being completely evicted in favor of a noisy session.

When a client attaches and its `lastSeq` is older than the ring's oldest entry, the coordinator responds with a `replay_unavailable` message. The phone shows the empty-state copy:

> "Even though no output is showing up, the session is connected. As you type and `<machine name>` responds, you will see new output here."

(or, if `state == paused`, "the session is disconnected. New output will appear when `<machine name>` reconnects.")

### 8.7 Routing

Daemon-originated session messages are routed to:

1. **All attached clients** for that session, as live event stream.
2. **The session's ring buffer**, for replay.
3. **The push system** if the event kind is in the push-trigger set and at least one client of this user has push enabled and is not currently attached to this session in the foreground.

(The "not currently attached in foreground" check requires a foreground/background heartbeat from the app, see § 11.4.)

Client-originated messages (pty input, attach, detach, spawn requests) are routed to:

- The relevant daemon (for that machine).
- For pty input: also echoed to other attached clients of the same session (so they see what was typed).

### 8.8 Push dispatch

When an event meets push criteria, the coordinator:

1. Builds a push payload (see § 11.4 schema).
2. For each push-eligible client of the user: encrypts the payload with that client's stored X25519 public key.
3. POSTs the ciphertext to ntfy at `<ntfy_url>/<client_topic>`.

`client_topic` is per-device, generated server-side at registration time (random 32-byte URL-safe string). The phone's UnifiedPush endpoint URL points at this topic. ntfy sees only the topic name and ciphertext bytes.

---

## 9. Android app (`pi-remote-android`)

### 9.1 Responsibilities

- Authenticate to coordinator via CF Access email-PIN.
- Maintain a WebSocket to coordinator while open or in foreground service mode (v1: foreground only when actively viewing; background relies on UnifiedPush wake-up).
- Display a list of machines and sessions, grouped by `(machine, project)`.
- Render attached session pty stream using Termux's terminal-emulator core.
- Send pty input back to coordinator.
- Receive UnifiedPush wake-ups, decrypt payload, surface as Android notification.
- Allow spawning new sessions on a chosen machine at a chosen cwd.

### 9.2 Project structure (Android Studio)

```
pi-remote-android/
├── app/
│   ├── build.gradle.kts
│   └── src/main/
│       ├── kotlin/dev/pi_remote/android/
│       │   ├── MainActivity.kt
│       │   ├── auth/                  # CF Access email-PIN flow, Keystore JWT
│       │   ├── net/                   # WebSocket client, reconnect
│       │   ├── push/                  # UnifiedPush registration, decrypt
│       │   ├── sessions/              # session list, attach UI
│       │   ├── terminal/              # TerminalEmulator integration
│       │   └── proto/                 # generated wire-protocol types
│       ├── res/
│       └── AndroidManifest.xml
├── build.gradle.kts
└── README.md
```

Dependencies:

- `com.termux:terminal-emulator` and `com.termux:terminal-view` (vendored or from Maven Central if published).
- `org.unifiedpush.android:connector` for UnifiedPush.
- `com.goterl:lazysodium-android` for `crypto_box`.
- OkHttp + Kotlin Coroutines for WebSocket.
- Standard AndroidX libs.

### 9.3 First-launch flow

1. App starts, finds no stored CF Access JWT in Keystore.
2. App opens an in-app `WebView` pointed at the coordinator URL, which redirects to CF Access email-PIN flow.
3. User enters their email, receives PIN, enters PIN. CF Access sets a cookie for the coordinator domain.
4. App captures the CF Access JWT from the cookie/header (CF Access exposes it as `CF_Authorization`), stores it in Keystore-backed `EncryptedSharedPreferences`.
5. App generates an X25519 keypair using libsodium. Stores private key in Keystore.
6. App registers a UnifiedPush distributor:
   - If a UnifiedPush distributor is installed (e.g., ntfy app), uses it. App receives a UnifiedPush endpoint URL.
   - If no distributor is installed, app prompts user to install ntfy from F-Droid or Play Store.
7. App calls `POST /clients/register` on the coordinator (using the JWT) with: client UUID, device display name, UnifiedPush endpoint URL, X25519 public key. Coordinator stores these and returns a per-device `client_id`.
8. App opens the main session-list WebSocket.

### 9.4 Session list UI

A list view grouped by machine, then by project. Sample structure:

```
─ MacBook Pro (online) ────────────────────────────
  ▾ angel-studios
      ● content-collections          running     last activity 12s ago
      ◌ content-watch-positions      idle        last activity 4h ago
  ▾ projects
      ◌ my-personal-pi               idle        last activity 1d ago

─ Desktop (suspended) ─────────────────────────────
  ⏸ angel-studios
      ⏸ pi-extensions                paused      lid closed at 14:32
```

State icons:
- ● running (agent actively streaming)
- ◌ idle (agent waiting for input)
- ⚠ attention (extension dialog open, tool failure, etc.)
- ⏸ paused (machine suspended)
- ✕ ended (Pi process exited)

Tapping a session opens the terminal view.

### 9.5 Terminal view

- Termux's `TerminalView` widget displays the pty stream.
- A `TerminalEmulator` instance maintains the VT state.
- Incoming pty chunks (decoded base64 from WebSocket frames) are written to the emulator buffer.
- Keyboard input from the user is sent as `pty_input` messages on the WebSocket.
- Standard terminal interactions: scroll, copy text via long-press, paste from clipboard.
- Soft keyboard accessory bar with Esc, Tab, Ctrl, Alt, arrow keys, and a slash (`/`) shortcut.
- A status bar at the top shows connection state (online / reconnecting / paused) and session metadata (project name, model).

### 9.6 Push notification handling

When ntfy delivers a UnifiedPush message to the ntfy distributor app, the distributor wakes pi-remote-android via a broadcast intent. The app's `BroadcastReceiver`:

1. Reads the ciphertext from the intent extras.
2. Loads the X25519 private key from Keystore.
3. Calls `crypto_box_open` with the coordinator's public key (which the app received during first-launch handshake) and the ephemeral nonce embedded in the payload.
4. Parses the resulting JSON.
5. Constructs an Android `Notification`:
   - Title: `<machine name>: <project name>`
   - Body: payload `summary` field
   - Tap action: deep-link to the session in the app
   - Notification channel: `attention` (importance HIGH)
6. Posts the notification.

Note: the app does **not** maintain a foreground service. It responds to wake-ups from UnifiedPush; the system wakes it briefly to handle each notification.

### 9.7 Foreground/background distinction for push suppression

To avoid "notification while I'm literally looking at the session in the app," the app reports its visibility to the coordinator over the WebSocket:

```json
{ "type": "client_focus", "sessionId": "...", "focused": true }
{ "type": "client_focus", "sessionId": "...", "focused": false }
```

The coordinator's push dispatch logic (§ 8.7) skips push to a client whose latest reported state is `focused: true` for the relevant session.

If the app is backgrounded or killed, the coordinator hasn't heard `focused: false` explicitly. To handle this, the WebSocket close event on the coordinator side implicitly flips all of that client's sessions to `focused: false`. So push works correctly when the app is fully backgrounded or killed (most common case).

---

## 10. Wire protocols

All protocols are JSON-over-WebSocket with one frame per message. Newline framing is not required because WebSocket frames are already discrete. The Unix socket protocol (extension ↔ daemon) uses LF-delimited JSONL.

Common envelope for all messages:

```json
{
  "type": "<message-type>",
  "v": 1,
  ... message-specific fields ...
}
```

`v` is the protocol version. v1 omits backward-compatibility code; future protocols can branch on this field.

Sequence numbers (`seq`) on session events are u64 monotonic per session, assigned at the daemon. They are **not** globally unique across sessions or machines.

### 10.1 Extension ↔ Daemon (Unix socket, JSONL)

Socket path: `~/.pi-remote/daemon.sock` (overridable via `PI_REMOTE_SOCKET` env var).

Authentication: filesystem permissions. Socket is created with mode 0600 owned by the user. Since both the daemon and the extension run as the same user, this is sufficient.

#### Extension → Daemon

**`register`** (first message after connect):

```json
{
  "type": "register",
  "v": 1,
  "session_id": "0ea51497613daf7e1de28ee99950b074",
  "spawn_token": "abc123" ,
  "cwd": "/Users/clayton/projects/foo",
  "project_name": "foo",
  "project_display_name": null,
  "tmux_target": "pi-remote-control:0.0",
  "pid": 12345,
  "hostname": "macbook-pro.local",
  "model": "anthropic/claude-sonnet-4-20250514",
  "started_at": 1730000000000
}
```

Daemon replies `register_ack` with `{"type":"register_ack","session_id":"...","accepted":true}` or `{"...","accepted":false,"reason":"..."}` if the registration is rejected (e.g., `session_id` already registered with a different pid).

**`event`** — projection of a Pi event:

```json
{
  "type": "event",
  "v": 1,
  "kind": "agent_end",
  "ts": 1730000000123,
  "data": {
    "messages_summary": {
      "count": 3,
      "last_text": "Done. The tests pass.",
      "tools_used": ["read", "edit", "bash"]
    }
  }
}
```

`kind` is one of: `agent_start`, `agent_end`, `attention_dialog`, `tool_failure`, `queue_update`, `model_select`, `compaction_start`, `compaction_end`, `extension_error`.

**`heartbeat`**:

```json
{ "type": "heartbeat", "ts": 1730000000999 }
```

**`disconnect`** (sent on `session_shutdown`):

```json
{ "type": "disconnect", "reason": "session_shutdown" }
```

#### Daemon → Extension

**`register_ack`**: see above.

**`ping`** (optional liveness):

```json
{ "type": "ping", "ts": 1730000000999 }
```

Reserved for future:

- `inject_message` (v2) — daemon asks extension to call `pi.sendUserMessage(...)` with attached image content.
- `extension_ui_response` (v3) — daemon delivers a tap-action response to a pending extension dialog.

### 10.2 Daemon ↔ Coordinator (WebSocket, JSON)

Endpoint: `wss://<coordinator-host>/v1/daemon`

Auth: HTTP headers `CF-Access-Client-Id` and `CF-Access-Client-Secret` set by the daemon's HTTP client; CF Access validates and forwards.

#### Daemon → Coordinator

**`machine_register`** (first message after WebSocket open):

```json
{
  "type": "machine_register",
  "v": 1,
  "machine_id": "macbook-pro",
  "machine_display_name": "MacBook Pro",
  "daemon_version": "1.0.0",
  "capabilities": ["spawn", "mirror"]
}
```

**`session_started`** — when the daemon learns of a new Pi session via extension `register`:

```json
{
  "type": "session_started",
  "v": 1,
  "session_id": "...",
  "machine_id": "macbook-pro",
  "metadata": {
    "cwd": "...",
    "project_name": "foo",
    "project_display_name": null,
    "hostname": "macbook-pro.local",
    "model": "anthropic/claude-sonnet-4-20250514",
    "started_at": 1730000000000,
    "spawn_token": "abc123"
  }
}
```

**`session_event`** — structured event:

```json
{
  "type": "session_event",
  "v": 1,
  "session_id": "...",
  "seq": 42,
  "kind": "agent_end",
  "ts": 1730000000123,
  "data": { ... }
}
```

**`session_pty`** — pty bytes:

```json
{
  "type": "session_pty",
  "v": 1,
  "session_id": "...",
  "seq": 43,
  "ts": 1730000000123,
  "bytes": "<base64>"
}
```

**`session_state_change`**:

```json
{
  "type": "session_state_change",
  "v": 1,
  "session_id": "...",
  "seq": 44,
  "ts": 1730000000123,
  "from": "running",
  "to": "idle"
}
```

**`session_ended`**:

```json
{
  "type": "session_ended",
  "v": 1,
  "session_id": "...",
  "seq": 45,
  "reason": "process_exit"
}
```

**`machine_suspending`**:

```json
{ "type": "machine_suspending", "v": 1, "ts": 1730000000123 }
```

**`session_resume`** — on reconnect, daemon sends one of these per still-live session:

```json
{
  "type": "session_resume",
  "v": 1,
  "session_id": "...",
  "metadata": { ... },
  "last_seq_emitted": 42
}
```

**`spawn_response`** — daemon's reply to a coordinator spawn request:

```json
{
  "type": "spawn_response",
  "v": 1,
  "request_id": "...",
  "success": true,
  "session_id": "...",
  "tmux_target": "pi-remote-abc:0.0"
}
```

#### Coordinator → Daemon

**`spawn_request`**:

```json
{
  "type": "spawn_request",
  "v": 1,
  "request_id": "...",
  "cwd": "/Users/clayton/projects/foo",
  "project_override": null
}
```

**`pty_input`** — input from a client to inject into a session:

```json
{
  "type": "pty_input",
  "v": 1,
  "session_id": "...",
  "client_id": "...",
  "bytes": "<base64>"
}
```

**`abort_session`** — terminate a Pi session (v2 may add granularity for "abort current agent run" vs "kill process"):

```json
{
  "type": "abort_session",
  "v": 1,
  "session_id": "...",
  "mode": "kill"
}
```

### 10.3 Coordinator ↔ App (WebSocket, JSON)

Endpoint: `wss://<coordinator-host>/v1/client`

Auth: `Cookie: CF_Authorization=<jwt>` (CF Access JWT obtained during email-PIN flow).

#### App → Coordinator

**`client_hello`** (first message after WebSocket open):

```json
{
  "type": "client_hello",
  "v": 1,
  "client_id": "<assigned during /clients/register>",
  "app_version": "1.0.0"
}
```

**`subscribe_machine_list`** — client wants ongoing updates of machines and their sessions:

```json
{ "type": "subscribe_machine_list", "v": 1 }
```

**`attach`** — attach to a specific session:

```json
{
  "type": "attach",
  "v": 1,
  "session_id": "...",
  "last_seq": 42
}
```

If `last_seq` is `0` or absent, the client receives all available history from the ring.

**`detach`**:

```json
{ "type": "detach", "v": 1, "session_id": "..." }
```

**`pty_input`**:

```json
{
  "type": "pty_input",
  "v": 1,
  "session_id": "...",
  "bytes": "<base64>"
}
```

**`spawn_session`**:

```json
{
  "type": "spawn_session",
  "v": 1,
  "request_id": "<client-generated UUID>",
  "machine_id": "macbook-pro",
  "cwd": "/Users/clayton/projects/foo",
  "project_override": null
}
```

**`client_focus`**:

```json
{ "type": "client_focus", "v": 1, "session_id": "...", "focused": true }
```

#### Coordinator → App

**`machine_list`** (initial state and updates):

```json
{
  "type": "machine_list",
  "v": 1,
  "machines": [
    {
      "machine_id": "macbook-pro",
      "machine_display_name": "MacBook Pro",
      "state": "online",
      "sessions": [
        {
          "session_id": "...",
          "metadata": { ... },
          "state": "running"
        }
      ]
    }
  ]
}
```

**`machine_state_change`**:

```json
{
  "type": "machine_state_change",
  "v": 1,
  "machine_id": "macbook-pro",
  "state": "suspended"
}
```

**`session_started`**, **`session_ended`**, **`session_state_change`** — same shape as the daemon→coordinator versions but include `machine_id`.

**`session_event`**, **`session_pty`** — forwarded from the daemon's messages of the same type. Only sent to clients that have called `attach` for the session.

**`replay_unavailable`** — sent in response to `attach` if the requested `last_seq` is older than the ring's oldest entry:

```json
{
  "type": "replay_unavailable",
  "v": 1,
  "session_id": "...",
  "earliest_available_seq": 100,
  "current_seq": 245
}
```

**`spawn_response`** — relayed from the daemon, includes `client_id` so only the requesting client gets it.

### 10.4 Push payload

POST `<ntfy_url>/<client_topic>` with body = ciphertext bytes.

Plaintext (encrypted with `crypto_box`):

```json
{
  "v": 1,
  "kind": "needs_attention",
  "machine_id": "macbook-pro",
  "machine_display_name": "MacBook Pro",
  "session_id": "...",
  "project_name": "content-collections",
  "project_display_name": "Content Collections",
  "reason": "extension_dialog",
  "summary": "Permission required: rm -rf node_modules",
  "ts": 1730000000123,
  "deep_link": "pi-remote://session/<session_id>"
}
```

`reason` is one of: `agent_idle`, `extension_dialog`, `tool_failure`, `queue_update`, `extension_error`, `compaction_complete`. The phone applies user-configurable filters to decide which reasons trigger a notification (see § 13).

Encryption: `crypto_box_easy(plaintext, nonce, recipient_pubkey, sender_seckey)`. The 24-byte nonce is randomly generated per message and prepended to the ciphertext. The recipient is the phone (its X25519 pubkey from registration); the sender is the coordinator (its X25519 keypair generated on first run, with the public part shared during phone registration).

Ciphertext format on the wire: `nonce (24 bytes) || ciphertext (variable) || mac (16 bytes)`. base64-encoding for HTTP transit is acceptable but ntfy's `application/octet-stream` content type is used to keep payload size minimal; the phone decodes raw bytes from the UnifiedPush message body.

---

## 11. Authentication & authorization

### 11.1 Daemon ↔ Coordinator (machines)

CF Access service tokens. Provisioning steps for each new machine:

1. In the CF Access dashboard, create a service token with name `pi-remote-daemon-<machine_id>`.
2. CF returns `Client ID` and `Client Secret`.
3. Add an Access Application policy that allows requests from this service token to reach the coordinator hostname.
4. On the machine, write the credentials to `/etc/pi-remote/service_token_id` and `/etc/pi-remote/service_token_secret` (mode 0600, owned by the daemon's user).
5. Restart the daemon. It picks up the credentials, sets `CF-Access-Client-Id` and `CF-Access-Client-Secret` headers on every coordinator request, and CF Access lets it through.

Revocation: delete the service token in CF dashboard. Daemon's next request fails 403; daemon retries with backoff and logs the auth failure.

### 11.2 App ↔ Coordinator (clients)

CF Access email-PIN. Provisioning:

1. CF Access policy on the coordinator's hostname allows requests from a specific email or email group (Clayton's email).
2. App opens the coordinator URL in a `WebView`. CF Access redirects to the email-PIN flow.
3. User enters email, gets PIN, submits PIN. CF Access sets a `CF_Authorization` cookie / JWT.
4. App reads the JWT from the cookie, stores in Android Keystore-backed encrypted prefs.
5. App attaches the JWT to all WebSocket and HTTP requests as the `Cookie: CF_Authorization=<jwt>` header.

JWT lifetime: configured in CF Access (default 24h, can be set to 30 days in the policy). When the JWT expires, the app re-runs the email-PIN flow.

Revocation: remove the user's email from the CF Access policy. Existing JWTs continue to work until expiry. For immediate revocation, set the JWT lifetime low (or use CF Access's "revoke" admin action which invalidates outstanding tokens).

### 11.3 Coordinator API endpoints requiring auth

| Endpoint | Auth | Purpose |
|----------|------|---------|
| `WSS /v1/daemon` | Service token | Daemon connection |
| `WSS /v1/client` | Email-PIN JWT | Client connection |
| `POST /v1/clients/register` | Email-PIN JWT | Initial client registration |
| `GET /v1/health` | None | Health check (returns 200 if server running) |

`POST /v1/clients/register` body:

```json
{
  "device_display_name": "Pixel 8",
  "unifiedpush_endpoint": "https://ntfy.example.com/abcdef123",
  "x25519_pubkey": "<base64>"
}
```

Returns:

```json
{
  "client_id": "<assigned UUID>",
  "coordinator_x25519_pubkey": "<base64>"
}
```

The phone stores `client_id` and `coordinator_x25519_pubkey` and uses them for subsequent operations.

---

## 12. Session lifecycle

### 12.1 States

```
                                     ┌────────┐
                                     │  none  │  (no session yet)
                                     └────┬───┘
                                          │  Pi process spawned (user or phone)
                                          │  + extension registers
                                          ▼
                          ┌──────────────────────┐
                          │       running        │
                          │  (agent streaming)   │
                          └─────┬───────────┬────┘
            agent_end           │           │   Pi process exits
            (no queue)          │           │
                                ▼           ▼
                      ┌────────────┐    ┌────────┐
                      │    idle    │    │ ended  │
                      │  (waiting  │    └────────┘
                      │  for input)│
                      └─────┬──────┘
                            │   user prompt or steer
                            └──── back to running

   At any state from running/idle, machine suspend transitions to:
                      ┌────────────┐
                      │  paused    │  (machine suspended)
                      └─────┬──────┘
                            │   machine resumes, daemon reconnects
                            └─── back to previous state (running or idle)

   At any state, missed heartbeats > 30s transitions to:
                      ┌──────────────┐
                      │ unresponsive │  (extension/daemon channel dead but
                      └──────────────┘   process possibly still running)
```

### 12.2 State source of truth

| State | Determined by |
|-------|---------------|
| `running` vs `idle` | Pi event sequence: `agent_start` → `running`; `agent_end` with empty queue → `idle`. |
| `paused` | Daemon's `machine_suspending` message sets all sessions on that machine to `paused`. |
| `unresponsive` | Daemon detects 3 consecutive missed heartbeats from extension. |
| `ended` | Daemon detects pid exit OR receives explicit `disconnect` from extension. |

The coordinator mirrors the daemon's view. Clients see whatever the coordinator has.

### 12.3 Push triggers vs state transitions

| Transition | Push fires? | Default reason |
|-----------|-------------|----------------|
| `running` → `idle` (agent_end) | **Yes** (configurable) | `agent_idle` |
| any → `attention_dialog` event | **Yes** (configurable) | `extension_dialog` |
| `tool_failure` event | **Yes** (configurable) | `tool_failure` |
| `queue_update` event | No (configurable, off by default) | `queue_update` |
| `running` → `paused` | No (configurable) | `machine_suspended` |
| `paused` → previous | No | — |
| any → `ended` | No (configurable) | `session_ended` |
| any → `unresponsive` | No (configurable) | `unresponsive` |

The phone has a settings screen exposing each reason as a toggle.

---

## 13. Project model

Project name resolution lives in the extension (§ 6.5). The coordinator and clients treat the project name as a string label, with no semantic meaning beyond grouping.

Display rules in the app's session list:

- Sessions with the same `(machine_id, project_name)` are grouped under a single header.
- Group header text: `project_display_name` if set, else `project_name`.
- Within a group, sessions are sorted by `last_event_ts` descending.
- Groups within a machine are sorted alphabetically.
- Machines are sorted by `machine_display_name` alphabetically, with offline/suspended machines below online ones.

A user can rename a session in the app (cosmetic-only metadata); the coordinator stores the user-set name keyed by `(client_id, session_id)` and exposes it through `session_metadata` updates. Rename does not propagate to the project name; it's per-session.

---

## 14. tmux integration details

### 14.1 Why control mode

`tmux -CC` exposes a stable, line-based protocol on stdin/stdout that allows a single client to:

- Issue commands (`new-session`, `kill-session`, `send-keys`, `display-message`).
- Receive structured notifications: `%output`, `%window-add`, `%window-close`, `%session-changed`, `%pane-mode-changed`, `%exit`, `%begin`, `%end`, `%error`, `%layout-change`.
- Multiplex across all sessions and panes through a single connection.

This is the same primitive that iTerm2's tmux integration uses. It's well-tested, version-stable across tmux releases, and doesn't require shell-out for each command.

### 14.2 Daemon's tmux client lifecycle

On daemon startup:

1. Check if the control-mode tmux session exists: `tmux has-session -t pi-remote-control` (a regular non-control-mode call).
2. If not: spawn `tmux -CC new-session -d -s pi-remote-control` and capture its stdin/stdout.
3. If yes: spawn `tmux -CC attach -t pi-remote-control` and use that.
4. Start two goroutines: one parsing notifications from stdout, one writing commands to stdin.

The control-mode "session" itself is just a placeholder; the daemon never runs anything in it. Real Pi sessions are spawned as separate tmux sessions named `pi-remote-<uuid>`.

### 14.3 Pi spawn flow

When the coordinator sends `spawn_request`:

1. Daemon generates a session UUID and spawn token.
2. Daemon sends to tmux: `new-session -d -s pi-remote-<uuid> -c <cwd> "env PI_REMOTE_SPAWN_TOKEN=<token> pi"`.
3. tmux creates the session and starts Pi in pane 0.
4. The Pi extension loads, opens the daemon socket, and registers with the spawn token.
5. Daemon correlates the `register` message's `spawn_token` with the pending spawn request and replies to the coordinator with `spawn_response`.

If Pi fails to start (e.g., binary not in PATH), the extension never registers. Daemon detects this via a 10s timeout and replies with `spawn_response { success: false, error: "..." }`. Daemon also kills the empty tmux session.

### 14.4 Local user interaction

A local user can attach to any Pi session by name:

```
tmux a -t pi-remote-<uuid>
```

Or list sessions:

```
tmux ls
```

The control-mode client and the user-attached client coexist as separate tmux clients in the same tmux server. User keystrokes go into the pane; the control-mode client sees `%output` for everything written to the pane (which includes echoed input).

The daemon's pty_input forwarding (from phone clients) goes through `send-keys` commands rather than direct pty writes. This ensures keystrokes are processed identically to local input.

### 14.5 Resize handling

When the phone client's terminal view resizes (e.g., user rotates device), the app sends a `pty_input` with a `\x1b[8;<rows>;<cols>t` (xterm CSI window manipulation), or alternately a structured `pty_resize` message:

```json
{ "type": "pty_resize", "v": 1, "session_id": "...", "rows": 40, "cols": 100 }
```

The daemon translates this into `tmux resize-window -t pi-remote-<uuid> -x <cols> -y <rows>` (or `resize-pane`).

Multi-attach with different sized clients: the smallest dimensions win (tmux's default). Local user attached at 200x60, phone at 100x40 → tmux clamps the pane to 100x40 across both. This is acceptable in v1; v2 may add per-client viewports.

---

## 15. Spawn-from-phone flow

Step-by-step:

1. User taps "+" in the app's session list view, picks machine `macbook-pro`, and either picks a recent cwd or types a new path. Optionally enters a project name override.
2. App sends `spawn_session` to coordinator with `request_id`, `machine_id`, `cwd`, `project_override`.
3. Coordinator looks up daemon for `macbook-pro`. If offline, returns error to app: `{ "type": "spawn_response", "request_id": "...", "success": false, "error": "machine offline" }`.
4. Coordinator forwards to daemon as `spawn_request`.
5. Daemon executes the tmux flow (§ 14.3). Generates spawn token, runs `new-session`, awaits extension registration with timeout.
6. On extension registration with matching spawn token, daemon completes the correlation and emits `session_started` (with `spawn_token` echoed in metadata so the coordinator can correlate).
7. Coordinator emits `session_started` to all subscribed clients. The originating client also receives `spawn_response { success: true, session_id }` keyed by `request_id`.
8. App auto-navigates to the new session (since the user just spawned it).

Failure modes:

- **Daemon offline:** coordinator replies immediately.
- **Pi binary not found / fails to start:** daemon timeout (10s), reply with `success: false, error: "pi did not register within 10s; check daemon logs"`.
- **Invalid cwd (does not exist):** tmux fails to start; daemon reports the tmux error.
- **Permissions issue:** same.

App displays a toast on failure with the error string.

---

## 16. Multi-attach semantics

### 16.1 Input

All attached clients (phone, tablet, local terminal via `tmux a`) have full input rights. Keystrokes from any source are interleaved into the pty in arrival order at the tmux server. With one user across multiple devices, racing your own input is a self-inflicted problem and accepted as such.

### 16.2 Output

All attached clients see every byte of pty output, including bytes echoed from input (since terminals echo by default). Two clients typing simultaneously would each see both sets of echoed characters interleaved; this matches local-only multi-attach behavior and is not specific to Pi Remote.

### 16.3 Detach

A client can detach without affecting others. Pi keeps running. The client's WebSocket close is sufficient signal to the coordinator; no explicit `detach` is required (though clients can send one for clean disconnect).

### 16.4 Last-attached-client policy

There is no special handling for "primary" clients. The daemon does not track who's attached at all — it just emits events. Tracking attached clients is the coordinator's job, and it uses the count only for push suppression (don't send a push for an event the user is currently watching live in the app).

---

## 17. Suspend handling

When the MacBook lid closes:

1. macOS sends `NSWorkspaceWillSleepNotification` to the daemon.
2. Daemon sends `machine_suspending` to coordinator and closes WebSocket gracefully.
3. macOS suspends the process. Pi processes in tmux are paused as part of the system suspend. tmux server is also paused.
4. Coordinator marks the machine as `suspended` and propagates `machine_state_change` to all subscribed clients.
5. Coordinator updates each session on that machine to state `paused` and propagates `session_state_change`.
6. Phone displays paused sessions with the pause icon and grayed-out terminal view. The terminal view shows last-cached pty content (whatever's in the ring) but is non-interactive.

When the lid opens:

1. macOS resumes the daemon process.
2. Daemon's WebSocket is dead (was closed before suspend). Reconnect logic kicks in.
3. Daemon connects, sends `machine_register`, then `session_resume` for each still-live Pi process (it can verify pids are still valid).
4. Coordinator transitions machine to `online` and sessions back to their pre-suspend state.
5. Phone re-enables interaction.

While paused:

- Phone shows last-cached output, no input accepted.
- No push notifications fire from this machine (no events are flowing).
- Other machines (desktop) are unaffected.

---

## 18. Coordinator broker (in-memory ring)

### 18.1 Sizing

- `total_cache_bytes`: 50MB hard cap across all sessions.
- `session_cache_floor_bytes`: 1MB minimum per active session before LRU evicts further (prevents complete eviction of quiet sessions).

In practice with two machines and ~5 active sessions each, this gives ~5MB per session at full saturation, which is plenty for a recent buffer of pty output and structured events. Heavy bursts (e.g., a tool that prints a lot) trigger LRU eviction of older entries within the same session and across less-active sessions.

### 18.2 Ring structure (per session)

```go
type RingBuffer struct {
    entries []Entry         // circular slice
    head    int             // next write position
    tail    int             // oldest entry position
    bytes   int             // current total size in bytes
    maxBytes int            // dynamic, depends on global LRU pressure
    earliestSeq uint64      // seq of oldest entry
    latestSeq   uint64      // seq of newest entry
}

type Entry struct {
    seq     uint64
    kind    EntryKind       // event or pty
    ts      int64
    payload []byte           // serialized JSON for events; raw bytes for pty
}
```

Append is O(1). On overflow (`bytes + entry > maxBytes`), oldest entries are dropped until it fits. Read for replay is a simple iteration from the entry whose `seq > requestedSeq`.

### 18.3 Global LRU coordinator

A separate goroutine periodically (every 5s or on append):

1. Sums `bytes` across all session rings.
2. If under cap, no action.
3. If over cap, picks the session with the oldest `LastTouched` and `bytes > session_cache_floor_bytes`, shrinks its `maxBytes` by 10%.
4. The shrunk session evicts entries on its next append.
5. Repeat until under cap.

If a session becomes "active" again (`LastTouched` updated), its `maxBytes` is allowed to grow back up to its share. The simplest growth policy: on each append for an active session, increase `maxBytes` by 10% if total cache is below 80% of cap.

### 18.4 Replay protocol

When a client `attach`es with `last_seq = N`:

1. If `N == 0`: stream all entries in the ring in seq order, then transition to live.
2. If `N >= ring.earliestSeq`: stream entries from `seq > N`, then live.
3. If `N < ring.earliestSeq`: send `replay_unavailable` with `earliest_available_seq` and `current_seq`, then transition to live (no backfill).

The phone's UI shows a "history not available" banner when `replay_unavailable` is received, but the terminal view still displays whatever output arrives live. The user can still interact.

---

## 19. Push notifications (UnifiedPush + Layer A E2EE)

### 19.1 Setup

User-side prerequisites:

- ntfy server running on UnraidOS in Docker, exposed via cloudflared tunnel at `ntfy.example.com`.
- ntfy Android app installed from F-Droid or Play Store, configured to subscribe to the same ntfy server.
- pi-remote-android installed and registered (which registers with UnifiedPush at first launch and provisions a topic on the ntfy server).

### 19.2 Registration flow

1. App calls UnifiedPush API: `UnifiedPush.registerApp(context)`.
2. UnifiedPush prompts the user to pick a distributor (only ntfy is installed → auto-selected after first prompt).
3. UnifiedPush returns an endpoint URL like `https://ntfy.example.com/up/abc123def456` (the path component is the per-device topic).
4. App stores the endpoint URL and includes it in `POST /v1/clients/register`.
5. Coordinator stores `endpoint_url` and `x25519_pubkey` keyed by `client_id`.

### 19.3 Push send flow

When the coordinator decides to push:

1. Build plaintext JSON payload (§ 10.4).
2. Generate a 24-byte random nonce.
3. Encrypt with `crypto_box_easy(plaintext, nonce, client.x25519_pubkey, coordinator.x25519_seckey)`.
4. Build wire format: `nonce || ciphertext || mac` as raw bytes.
5. POST to `<endpoint_url>` (which already includes the topic path) with `Content-Type: application/octet-stream`, body = wire bytes.
6. ntfy server stores the message and pushes it to the ntfy distributor app on the phone via ntfy's persistent subscription.
7. ntfy distributor app receives and delivers the bytes to pi-remote-android via UnifiedPush broadcast.
8. pi-remote-android decrypts, parses, and shows a notification.

### 19.4 Decryption on phone

```kotlin
// In a UnifiedPush BroadcastReceiver
fun onMessage(context: Context, message: ByteArray, instance: String) {
    val nonce = message.sliceArray(0 until 24)
    val ciphertextWithMac = message.sliceArray(24 until message.size)
    val plaintext = sodium.cryptoBoxOpenEasy(
        ciphertextWithMac,
        nonce,
        coordinatorPubkey,    // stored at registration
        myPrivkey             // from Keystore
    )
    val payload = Json.decodeFromString<PushPayload>(plaintext.toString(Charsets.UTF_8))
    showNotification(payload)
}
```

### 19.5 Notification rendering

Android `NotificationChannel`s:

- `attention` (importance HIGH, vibration, sound): `extension_dialog`, `tool_failure`, `agent_idle`.
- `info` (importance DEFAULT, no sound): `queue_update`, `compaction_complete`.
- `alert` (importance HIGH): `extension_error`, `unresponsive`.

Each notification:

- Title: `<machine_display_name> · <project_display_name or project_name>`
- Body: `summary` field of payload.
- Action: tap → deep link via `pi-remote://session/<session_id>` → opens MainActivity → navigates to terminal view for that session.
- Sub-text: `reason` rendered in human form ("Agent waiting", "Permission needed", "Tool failed").

### 19.6 User-configurable filters

The app's settings include a per-`reason` toggle:

- Agent idle → push? **on by default**
- Extension dialog → push? **on by default**
- Tool failure → push? **on by default**
- Queue update → push? **off by default**
- Compaction complete → push? **off by default**
- Extension error → push? **off by default**
- Unresponsive → push? **on by default**
- Session ended → push? **off by default**

Filter changes propagate to the coordinator via `POST /v1/clients/<client_id>/preferences`. The coordinator applies them at push-decision time so it doesn't waste round-trips for filtered events.

---

## 20. Roadmap

### v1 — ship target

Everything in this spec. Ordered by dependency:

1. **Pi extension scaffolding.** Empty extension, registers with daemon over Unix socket, heartbeat. No event forwarding yet.
2. **Daemon scaffolding.** Listens on Unix socket, accepts extension registrations, basic state tracking. tmux control mode connection. No coordinator connection yet.
3. **Coordinator scaffolding.** WebSocket endpoint with CF Access middleware. Health check. No business logic yet.
4. **Daemon ↔ Coordinator wire.** Daemon connects, registers machine, sends and receives basic messages. End-to-end smoke test.
5. **Extension event forwarding.** Hook all Pi events listed in § 6.4. Daemon forwards to coordinator.
6. **Pty mirroring.** Daemon reads `%output` from tmux control mode and forwards as `session_pty` to coordinator. Coordinator broadcasts to attached clients.
7. **Coordinator broker / ring buffer.** Per-session ring with global LRU.
8. **Spawn-from-coordinator.** `spawn_request`/`spawn_response` flow.
9. **Suspend detection.** macOS and Linux variants.
10. **Android app shell.** Login flow with CF Access email-PIN. Session list view with mock data.
11. **Android terminal view.** Termux integration. Display pty stream from coordinator.
12. **Android pty input.** Send keystrokes back. Resize handling.
13. **Push setup.** Coordinator generates X25519 keypair on first run. Phone keypair generation, registration. Ntfy server in Docker on unraid.
14. **Push send/receive.** Encrypt on coordinator, decrypt on phone, render notification.
15. **Push filters.** Per-reason settings UI on phone, propagation to coordinator.
16. **End-to-end test.** Real flow on real hardware.
17. **Docs.** Install guides for daemon (macOS launchd, Linux systemd), coordinator (docker-compose), Android (sideload or self-built APK).

Estimated effort: 6–10 solid weekends, depending on Kotlin/Termux familiarity.

### v2 — fast follows

- **Image attachment from phone.** Add `inject_message` to extension protocol, app UI for picking image, daemon → extension routing, extension calls `pi.sendUserMessage` with image content block.
- **Session traffic E2EE (Layer B).** Noise XK handshake between daemon and each attached client (relayed through coordinator as opaque blobs). ChaCha20-Poly1305 with periodic rekey. Updates to broker to cache ciphertext.
- **LAN shortcut.** Phone discovers coding machines on the local network via mDNS; if found, prefer direct connection (still authenticated via service-token-derived shared secret, or possibly Tailscale).
- **Persistent broker history.** Optional disk-backed event log on coordinator for resume across restart.
- **Wake-on-LAN for desktop** (not MacBook).

### v3 — quality of life

- **Tap-to-respond from notifications.** Lock-screen actions for `confirm` dialogs. Adds `extension_ui_response` to extension protocol.
- **Multi-tab terminal in app.** Multiple session views in a tabbed UI for fast switching.
- **Search across sessions** (text search on cached pty bytes and structured events).
- **Audit log on coordinator.** Persistent record of who attached when and what was typed.
- **Cost tracker.** Surface session cost from `agent_end` events in the app.

---

## 21. Implementation decisions

These were called out as deferred during the design discussion and are now decided. Implementation must follow these directly; deviations require an ADR in `pi-remote-spec/adrs/`.

| # | Decision area | Resolution |
|---|---------------|-----------|
| D1 | Wire-type source of truth | JSON Schema (Draft 2020-12) files in `pi-remote-spec/protocol/` are authoritative. Each component codegens types from these into a `proto/` (or language-equivalent) directory. Generated files carry a header `// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/...`. Hand-edited types are forbidden. |
| D2 | Termux library distribution | Vendored. `terminal-emulator` and `terminal-view` are Apache 2.0 but not published to Maven Central. Copy the source into `pi-remote-android/vendor/` as Gradle modules `:terminal-emulator` and `:terminal-view`. Track upstream commit SHA in `vendor/UPSTREAM.md`. Apache 2.0 NOTICE preserved. |
| D3 | macOS suspend detection | cgo binding to Foundation framework, subscribing to `NSWorkspaceWillSleepNotification` and `NSWorkspaceDidWakeNotification`. Implementation in `internal/suspend/darwin.go` behind a `//go:build darwin` tag. Fallback non-suspend stub for other platforms in same package. |
| D4 | Linux suspend detection | `github.com/godbus/dbus/v5` subscribing to the `PrepareForSleep` signal on `org.freedesktop.login1.Manager`. Implementation in `internal/suspend/linux.go` behind `//go:build linux`. |
| D5 | CF Access auth flow on Android | Chrome Custom Tab via `androidx.browser:browser`, with App Links callback at `pi-remote://auth/callback`. JWT extracted from the `CF_Authorization` cookie via the cookie store after callback. Stored in `EncryptedSharedPreferences` (AndroidX Security Crypto). In-app `WebView` is NOT used for auth (cookie-isolation issues). |
| D6 | Spawn token | 16 bytes from `crypto/rand`, hex-encoded as a 32-character string. Passed via env var `PI_REMOTE_SPAWN_TOKEN`. Daemon retains the token for 30s after spawn; if no extension registers within that window with a matching token, daemon kills the tmux session and reports failure to the coordinator. |
| D7 | tmux server resilience | Daemon detects control-mode connection close (stdin EOF or `%exit` notification). On detection: clear in-memory session state, mark all sessions ended to coordinator with reason `tmux_server_lost`, attempt reconnect every 5 seconds. On reconnect, daemon does NOT try to recover prior sessions; user must spawn fresh ones. |
| D8 | Terminal title spoofing | Daemon strips OSC 0, 1, and 2 sequences (xterm title-set sequences) from outbound pty bytes before forwarding. Strip patterns: `ESC ] (0\|1\|2) ; <text> BEL` and `ESC ] (0\|1\|2) ; <text> ESC \`. Implementation in `internal/ptymux/sanitize.go` with unit tests for both terminator forms. |
| D9 | Bandwidth compression | None in v1. Pty bytes and event payloads are sent uncompressed over WebSocket. v2 may add zstd at the daemon→coordinator and coordinator→app legs after profiling shows it matters. The reserved `compression` protocol field (§ 24) is for forward-compat. |
| D10 | CF Access for long-lived WebSockets | Use Cloudflare Tunnel directly to a TCP/HTTP origin behind CF Access. Avoid CF Workers in front of WebSocket endpoints (Workers have request duration limits). Tunnel + Access supports long-lived WebSockets natively. |
| D11 | Daemon process supervision | macOS: `launchd` user agent with `KeepAlive=true` and `RunAtLoad=true`. Linux: `systemd --user` unit with `Restart=always` and `RestartSec=5`. Plist and unit files committed to `pi-remote-daemon/deploy/`. |
| D12 | Logger | All Go services use `log/slog` (stdlib) with JSON output. Rotation via `gopkg.in/natefinch/lumberjack.v2`. Default verbosity: info. Debug under env `PI_REMOTE_DEBUG=1`. Android uses Android `Log` with tag prefix `pi-remote/<module>`. |
| D13 | Service token credential storage on macOS | Files `/etc/pi-remote/service_token_id` and `/etc/pi-remote/service_token_secret`, mode 0600, owned by root and readable by the daemon's user via group membership. Setup script in `deploy/install-macos.sh` provisions these. macOS Keychain integration is deferred (avoid platform-specific bindings in v1). |
| D14 | Service token credential storage on Linux | Same file convention as macOS. systemd `LoadCredential` directive may replace this in v2 as a hardening pass. |
| D15 | Coordinator deployment | Single Docker container running the coordinator binary. Image based on `gcr.io/distroless/static`. Compose file at `pi-remote-coordinator/deploy/docker-compose.yaml` runs alongside ntfy on the same Docker network. Healthcheck: `GET /v1/health`. |
| D16 | Coordinator persistence in v1 | None. All state in memory. On restart, daemons reconnect and re-register all sessions; clients reconnect and re-attach. Ring buffers re-fill from scratch. Acceptable v1 behavior; v2 may add disk-backed event log. |
| D17 | UUID format | All IDs (session, request, client, machine) are UUIDv7 (`github.com/google/uuid` v1.6+ provides `NewV7()`). Timestamp-prefixed for natural sort. String representation uses canonical form (8-4-4-4-12 hex with hyphens). |
| D18 | Sequence number lifecycle | Per-session `seq` counter starts at 1 and increases monotonically for the session's lifetime. Preserved across daemon restart by including `last_seq_emitted` in `session_resume`. If the daemon truly loses the counter (unclean kill), it starts at `last_known + 1000` to avoid collision with cached coordinator entries. |
| D19 | Heartbeat intervals | Extension → daemon: every 10s. Daemon → coordinator: every 30s (TCP keepalive primary). Coordinator → client: every 30s. Timeout: 3 missed heartbeats marks the connection dead. |
| D20 | Project name resolution timing | Computed by extension on `session_start` and sent in `register`. Daemon and coordinator do not recompute. Changes to `.pi-remote.toml` mid-session take effect only on next `session_start`. |
| D21 | Codegen invocation | Each component's `scripts/codegen.sh` clones or updates the `pi-remote-spec` repo at a pinned commit (recorded in `scripts/spec-version.txt`), runs the language-specific generator, and writes outputs into `proto/` (or equivalent). Bumping the pinned commit is a deliberate PR. |
| D22 | License headers | Each source file in each repo carries a one-line header: `// SPDX-License-Identifier: MIT` (or `# SPDX-License-Identifier: MIT` for shell/Python/TOML). No copyright notice required at file level; LICENSE file at repo root is the authoritative copyright. |
| D23 | Branch protection | Each repo's `main` branch requires PR + 1 approval (self-approval acceptable for solo work) + passing CI. Configured via `gh repo edit` after creation. |
| D24 | Conventional commits | Commit messages follow Conventional Commits format: `feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`. Enforced via PR title check in CI; not enforced on individual commits. |
| D25 | Generated code in PRs | Codegen output is committed to git (not gitignored). PRs that change schemas in `pi-remote-spec` and bump the pinned commit in dependent repos must include the regenerated output in the same PR. CI verifies generated files are up-to-date by re-running codegen and diffing. |

---

## 22. Library choices and pinning

This section pins concrete libraries and versions per component. Implementation must use these unless an ADR documents the exception.

### 22.1 Pi extension (`pi-remote-ext`, TypeScript)

| Concern | Library | Version |
|---------|---------|---------|
| Runtime | Node.js | ≥ 22 LTS (matches Pi's runtime) |
| Pi extension API | `@mariozechner/pi-coding-agent` | latest at install time; track Pi releases |
| Tool param schemas | `typebox` | ^0.34 |
| Unix socket | Node `net` (stdlib) | — |
| TOML parsing for `.pi-remote.toml` | `smol-toml` | ^1.3 |
| JSON Schema validation | `ajv` | ^8.17 |
| JSON Schema → TS codegen | `json-schema-to-typescript` | ^15.0 (devDep) |
| Test runner | `vitest` | ^2.1 |
| Linter / formatter | `biome` | ^1.9 |

`package.json` declares the Pi extension via `{"pi": {"extensions": ["./src/index.ts"]}}`.

### 22.2 Daemon (`pi-remote-daemon`, Go)

| Concern | Library | Version |
|---------|---------|---------|
| Go toolchain | — | 1.25.x (or current stable) |
| WebSocket client | `github.com/coder/websocket` | v1.8.x |
| TOML config | `github.com/BurntSushi/toml` | v1.4.x |
| Logging | `log/slog` (stdlib) | — |
| Log rotation | `gopkg.in/natefinch/lumberjack.v2` | v2.2.x |
| D-Bus (Linux suspend) | `github.com/godbus/dbus/v5` | v5.1.x |
| JSON Schema validation | `github.com/santhosh-tekuri/jsonschema/v6` | v6.0.x |
| JSON Schema → Go codegen | `github.com/atombender/go-jsonschema` | latest CLI |
| UUIDv7 | `github.com/google/uuid` | v1.6.x |
| Test assertions | `github.com/stretchr/testify` | v1.10.x |
| Linter | `golangci-lint` | v1.62+ |

### 22.3 Coordinator (`pi-remote-coordinator`, Go)

Shares Go toolchain and core libraries with the daemon. Additions:

| Concern | Library | Version |
|---------|---------|---------|
| HTTP routing | stdlib `net/http` (Go 1.22+ ServeMux) | — |
| WebSocket server | `github.com/coder/websocket` | v1.8.x |
| ntfy HTTP client | stdlib `net/http` | — |
| NaCl crypto for push | `golang.org/x/crypto/nacl/box` | latest |
| In-memory ring buffer | own implementation in `internal/broker/` | — |

### 22.4 Android app (`pi-remote-android`, Kotlin)

| Concern | Library | Version |
|---------|---------|---------|
| Kotlin | — | 2.1.x |
| Android Gradle Plugin | — | 8.7+ |
| Min SDK | — | 26 (Android 8.0) |
| Target SDK | — | 35 (Android 15) |
| Compose BOM | `androidx.compose:compose-bom` | 2024.12.x |
| Coroutines | `org.jetbrains.kotlinx:kotlinx-coroutines-android` | 1.9.x |
| Serialization | `org.jetbrains.kotlinx:kotlinx-serialization-json` | 1.7.x |
| WebSocket | `com.squareup.okhttp3:okhttp` | 4.12.x |
| Custom Tab (auth) | `androidx.browser:browser` | 1.8.x |
| Encrypted prefs | `androidx.security:security-crypto` | 1.1.0-beta01 |
| UnifiedPush | `org.unifiedpush.android:connector` | 3.0.x |
| libsodium | `com.goterl:lazysodium-android` | 5.1.x |
| JSON Schema → Kotlin codegen | `quicktype` (CLI) | latest |
| Termux terminal modules | vendored per D2 | upstream pinned commit |
| Testing | JUnit 5 + Robolectric + Espresso | current |

### 22.5 Spec repo (`pi-remote-spec`, tooling)

| Concern | Tool | Version |
|---------|------|---------|
| JSON Schema linter | `check-jsonschema` (Python via pipx) | latest |
| Multi-language codegen reference | `quicktype` | latest |
| Schema validation in CI | `ajv-cli` | ^5 |

### 22.6 Cross-cutting

- **CI:** GitHub Actions on every repo. A `ci.yml` workflow runs build + test + lint on every PR. Templates committed in Phase 0 (§ 23).
- **License:** MIT for all five repos. Standard MIT template, copyright holder `Clayton`. `LICENSE` at root of each repo.
- **Pre-commit:** Optional `.pre-commit-config.yaml` with formatting hooks committed but not required to pass CI.
- **EditorConfig:** `.editorconfig` at root of each repo with sensible defaults (LF endings, UTF-8, trim trailing whitespace, final newline).

---

## 23. Phase 0 agent task brief

This section is the literal task contract for an AI agent that will bootstrap the project. The user pastes a pointer to this section (or the full spec) into an agent session with GitHub CLI authenticated. The agent's job is to produce all the bootstrap artifacts so per-component implementation can proceed independently. **Do not modify this section's content during implementation; it is the contract.**

### 23.1 Mission

Bootstrap the Pi Remote project. Read the entire spec (`pi-remote-spec.md`) before starting. Produce the artifacts listed in § 23.4. Create five GitHub repositories. Push initial commits with bootstrap files. **Do not implement application logic** — that is for Phase 1 agents. Only skeletons that compile.

### 23.2 Inputs

- **The spec** — this entire document.
- **GitHub user** — `TheTechChild`.
- **License** — MIT for all repos. Copyright holder: `Clayton`.
- **Library choices** — § 22 is authoritative.
- **Implementation decisions** — § 21 is authoritative.

### 23.3 What to ask vs decide vs defer

- **Decide** anything covered by § 21, § 22, or § 23. Do not ask the user about library versions, naming conventions, or structural decisions described here.
- **Ask** only if the spec is genuinely ambiguous on a load-bearing decision. Phrase as "Spec § X says A but § Y implies B; which controls?" Do not ask trivial or stylistic questions.
- **Defer** anything that's not bootstrap work. Do not implement protocol message handling, do not write tests for non-existent code, do not bring up the daemon's tmux integration. **Skeletons only.**

### 23.4 Tasks

Execute in order. Each task ends with the agent committing and pushing.

#### Task 1: `pi-remote-spec` repo

1. Create `github.com/TheTechChild/pi-remote-spec` (public, MIT, default branch `main`).
2. Commit `SPEC.md` (a copy of `pi-remote-spec.md`) at repo root.
3. Create directory structure:
   ```
   pi-remote-spec/
   ├── SPEC.md
   ├── README.md
   ├── LICENSE
   ├── .editorconfig
   ├── .github/workflows/ci.yml
   ├── protocol/
   │   ├── extension-daemon/
   │   ├── daemon-coordinator/
   │   ├── coordinator-app/
   │   └── push/
   ├── fixtures/
   ├── scenarios/
   ├── errors/
   │   └── codes.md
   └── adrs/
       └── 0000-template.md
   ```
4. Generate JSON Schema files (Draft 2020-12, one per message type) for every wire message in §§ 10.1–10.4. Filename = message `type` field (e.g., `register.json`, `session_event.json`). Group by direction under `protocol/<leg>/`.
5. Generate `errors/codes.md` with a table of error codes used in protocol responses. Format: `ERR_<COMPONENT>_<CONDITION>`. Include description and which message types may return each. Derive comprehensively from the spec.
6. Generate canonical fixture exchanges in `fixtures/`:
   - `extension-register-flow.jsonl` — extension registers with daemon, ack, three heartbeats, `agent_start`, `agent_end`, `disconnect`.
   - `daemon-coordinator-handshake.jsonl` — daemon connects, `machine_register`, `session_started` for one session, three `session_event`s, `session_pty` chunks, `session_state_change`, `machine_suspending`.
   - `client-attach-flow.jsonl` — `client_hello`, `subscribe_machine_list`, `machine_list` response, `attach`, replay, `pty_input` round-trip.
   - `spawn-from-phone-flow.jsonl` — `spawn_session` from client, `spawn_request` to daemon, `spawn_response`, `session_started` broadcast.
   - `push-payload-plaintext.json` — example unencrypted push payload conforming to its schema.
7. Generate `scenarios/` markdown files describing end-to-end flows referencing fixtures:
   - `attention-notification.md`
   - `multi-attach.md`
   - `machine-suspend.md`
   - `replay-on-reconnect.md`
   - `replay-evicted.md`
8. Add `adrs/0000-template.md` with sections: status, context, decision, consequences, references.
9. Add `README.md` linking to `SPEC.md` and listing the four component repos.
10. Add CI workflow that runs `check-jsonschema` against every file in `protocol/` and validates each fixture line against its corresponding schema.

**Acceptance for Task 1:**
- All schemas validate as Draft 2020-12.
- Each fixture record validates against its corresponding schema.
- CI passes on initial commit.

#### Task 2: `pi-remote-ext` repo

1. Create repo, MIT.
2. Bootstrap files:
   - `package.json` per § 22.1, with the Pi extension declaration.
   - `tsconfig.json` strict, target ES2022, module ESNext.
   - `biome.json` for lint/format.
   - `vitest.config.ts`.
   - `.gitignore` (Node), `.editorconfig`.
   - `.github/workflows/ci.yml` running install + lint + typecheck + test.
   - `src/index.ts` — default-export factory function as no-op skeleton (logs "pi-remote-ext loaded" on `session_start`).
   - `src/proto/` — codegen output via `json-schema-to-typescript`.
   - `scripts/codegen.sh` and `scripts/spec-version.txt`.
   - `LICENSE`, `README.md` with install instructions: `pi install git:github.com/TheTechChild/pi-remote-ext`.
3. Open issues, one per milestone derived from § 6:
   - M1: Unix socket connection with reconnect
   - M2: Registration handshake
   - M3: Pi event projection and forwarding
   - M4: Heartbeat loop
   - M5: Project name resolution (cwd parent + git root + `.pi-remote.toml`)
   - M6: Spawn-token correlation
   - M7: Graceful disconnect on `session_shutdown`
4. Each issue body links the relevant spec section and includes an acceptance checklist.

**Acceptance for Task 2:**
- `npm install && npm run build && npm test` succeeds on a clean clone.
- CI passes.
- Issues M1–M7 opened.

#### Task 3: `pi-remote-daemon` repo

1. Create repo, MIT.
2. Bootstrap:
   - `go.mod` per § 22.2.
   - Standard layout: `cmd/pi-remote-daemon/`, `internal/`, `deploy/`, `scripts/`.
   - `cmd/pi-remote-daemon/main.go` — reads config, logs version, exits 0 (skeleton).
   - `internal/proto/` — codegen output via `go-jsonschema`.
   - `internal/config/` — TOML loader skeleton.
   - `scripts/codegen.sh`, `scripts/spec-version.txt`.
   - `deploy/launchd/dev.pi-remote.daemon.plist` template.
   - `deploy/systemd/pi-remote-daemon.service` template.
   - `deploy/install-macos.sh` and `deploy/install-linux.sh` skeletons that print "TODO".
   - `.github/workflows/ci.yml` running `go build`, `go test`, `golangci-lint run`.
   - `.gitignore` (Go), `.editorconfig`, `LICENSE`, `README.md`.
3. Open issues per § 7's milestones:
   - M1: Unix socket listener with extension registration
   - M2: tmux control mode connection and parser
   - M3: Coordinator WebSocket client with auth + reconnect
   - M4: Session registry and event multiplexing
   - M5: Pty mirroring via `%output`
   - M6: Spawn handler (`spawn_request` → tmux + spawn-token correlation)
   - M7: Suspend detection (macOS, cgo Foundation)
   - M8: Suspend detection (Linux, godbus PrepareForSleep)
   - M9: Reconnect logic with `session_resume`
   - M10: Title-spoof sanitizer (D8)

**Acceptance for Task 3:** `go build ./...` and `go test ./...` succeed. CI passes.

#### Task 4: `pi-remote-coordinator` repo

1. Create repo, MIT.
2. Bootstrap:
   - `go.mod` per § 22.3.
   - `cmd/pi-remote-coordinator/main.go` — starts an HTTP server on `:8080` with `/v1/health` returning 200.
   - `internal/proto/`, `internal/config/`, `internal/broker/` (skeleton with type stubs only).
   - `deploy/Dockerfile` (distroless static base).
   - `deploy/docker-compose.yaml` running coordinator + ntfy on a Docker network.
   - `scripts/codegen.sh`, `scripts/spec-version.txt`.
   - `.github/workflows/ci.yml` running `go build`, `go test`, `golangci-lint`, plus a Docker build smoke test.
   - `.gitignore`, `.editorconfig`, `LICENSE`, `README.md` with deploy instructions for unraid.
3. Open issues per § 8's milestones:
   - M1: Daemon WebSocket endpoint with CF service-token auth
   - M2: Client WebSocket endpoint with CF Access JWT auth
   - M3: Machine and session registry
   - M4: Broker (per-session ring + global LRU)
   - M5: Push dispatch (NaCl encrypt + ntfy POST)
   - M6: Client preferences endpoint
   - M7: Replay protocol and `replay_unavailable`
   - M8: Foreground/background tracking for push suppression
   - M9: Coordinator keypair generation on first run

**Acceptance for Task 4:** `docker compose up` starts coordinator + ntfy. `curl http://localhost:8080/v1/health` returns 200. CI passes.

#### Task 5: `pi-remote-android` repo

1. Create repo, MIT.
2. Bootstrap:
   - Android Studio project with `:app` module.
   - Vendored `:terminal-emulator` and `:terminal-view` Gradle modules under `vendor/`. Pin to a specific commit of termux-app and record the SHA in `vendor/UPSTREAM.md`. Preserve the Apache 2.0 NOTICE.
   - Versions per § 22.4.
   - `MainActivity.kt` Compose skeleton showing a placeholder "Pi Remote" screen.
   - `AndroidManifest.xml` with permissions: INTERNET, POST_NOTIFICATIONS. UnifiedPush manifest entries.
   - App Link intent filter for `pi-remote://auth/callback` (D5).
   - `app/src/main/kotlin/dev/pi_remote/android/proto/` — codegen output via `quicktype`.
   - `scripts/codegen.sh`, `scripts/spec-version.txt`.
   - `.github/workflows/ci.yml` running `./gradlew lint test assembleDebug`.
   - `.gitignore`, `.editorconfig`, `LICENSE`, `README.md` with build instructions.
3. Open issues per § 9's milestones:
   - M1: CF Access auth flow via Custom Tab + App Link callback
   - M2: Client registration HTTP call after auth
   - M3: Session list UI (Compose) with mock data
   - M4: Termux terminal-view integration in a session screen
   - M5: WebSocket client to coordinator
   - M6: Pty input from terminal-view to WebSocket
   - M7: UnifiedPush registration flow
   - M8: Push payload decryption
   - M9: Notification rendering with deep-link to session
   - M10: Client preferences UI (per-reason push toggles)
   - M11: Resize handling

**Acceptance for Task 5:** `./gradlew assembleDebug` produces a working APK. App launches and shows the placeholder. CI passes.

### 23.5 Conventions for all tasks

- **Branch protection:** require PR + 1 approval + passing CI on `main` (D23). Configure via `gh repo edit` after each repo is created.
- **Generated files:** every codegen output file has a header comment `// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/...` (or `# GENERATED...` for non-C-style languages). Include source schema path.
- **License headers:** every source file has SPDX header per D22.
- **README structure:** Overview (1 paragraph), Spec link, Setup, Build, Test, License.
- **Issue titles:** prefix `M<n>:`. Body links the spec section and includes acceptance checklist.
- **Commit messages:** Conventional Commits (D24). Initial commit per repo: `chore: initial bootstrap`.

### 23.6 Final report

When all five tasks complete, produce a summary report containing:

1. URLs of all five repos.
2. Total number of issues opened per repo (with M-number ranges).
3. Any decisions you made that were not covered by the spec — these need new ADRs in `pi-remote-spec/adrs/` numbered sequentially from `0001-*.md`. List them.
4. Any spec ambiguities you hit and resolved by choice — flag for human review.
5. Any tasks where acceptance criteria failed (CI red, build broken, etc.) — flag for human attention.

### 23.7 Human checkpoint after Phase 0

Before any Phase 1 agent starts, the user must spend approximately one hour reviewing:

1. The JSON schemas in `pi-remote-spec/protocol/` — sanity-check against the prose in spec § 10.
2. The fixtures — verify they're realistic and match expected behavior.
3. One component repo's bootstrap — pick whichever was most decision-heavy in the agent's report.
4. The list of new ADRs and ambiguities flagged in § 23.6.

Approval = the user merges any pending PRs in the spec repo and gives the green light for Phase 1.

---

## 24. Reserved protocol fields

These fields are reserved in the v1 protocol but unused. Implementations should ignore them on receive. They will be activated in v2/v3.

- `inject_message` (daemon ↔ extension, daemon ↔ coordinator, coordinator ↔ app): for v2 image attach.
- `extension_ui_response` (daemon ↔ extension, daemon ↔ coordinator, coordinator ↔ app): for v3 tap-to-respond.
- `e2ee_handshake` and `e2ee_envelope` (daemon ↔ coordinator ↔ app): for v2 Layer B session E2EE.
- `compression` field on `session_pty`: for v2 zstd. v1 is always uncompressed.

---

## 25. Glossary

| Term | Meaning |
|------|---------|
| Pi | The terminal-based coding agent at pi.dev. |
| Pi extension | A TypeScript module loaded by Pi at startup that hooks events, registers tools, etc. |
| Pi session | One running Pi process with its persistent JSONL session file. |
| Daemon | The Go process on each coding machine that bridges Pi sessions to the coordinator. |
| Coordinator | The Go process on UnraidOS that brokers between daemons and clients. |
| Coding machine | A machine where Pi is run for actual development work (desktop or laptop). |
| Client | A device that attaches to a Pi session for monitoring/interaction (phone, tablet). |
| Pty | Pseudo-terminal — the OS abstraction Pi runs inside. |
| tmux control mode | tmux's `-CC` flag, exposing a stable command/notification protocol on stdin/stdout. |
| Cloudflare Access | CF's zero-trust auth layer, used for both service-token (machines) and email-PIN (clients) auth. |
| Cloudflare Tunnel / cloudflared | CF's outbound-only tunnel daemon, used so neither coding machines nor unraid expose public ports. |
| UnifiedPush | An open-standard alternative to FCM for push notifications on Android. |
| ntfy | A self-hostable pub/sub server, used here as a UnifiedPush distributor backend. |
| NaCl `crypto_box` | The libsodium primitive for authenticated public-key encryption (X25519 + XSalsa20-Poly1305). |
| Layer A E2EE | Encryption of push notification payloads (in scope for v1). |
| Layer B E2EE | Encryption of live session traffic (deferred to v2). |
| Spawn token | A random secret passed via env var by the daemon to Pi at spawn time, included in the extension's registration to correlate with the spawn request. |
| ADR | Architecture Decision Record — a short markdown document capturing a decision made during implementation that wasn't predetermined by the spec. |
| Phase 0 | Bootstrap phase: spec finalization, repo creation, scaffolding (§ 23). |
| Phase 1 | Per-component implementation phase, post-Phase-0. |

---

## 26. References

- Pi documentation: https://pi.dev/docs/latest
- Pi extension API: https://pi.dev/docs/latest/extensions
- Pi RPC mode (referenced for context): https://pi.dev/docs/latest/rpc
- Pi session format: https://pi.dev/docs/latest/session-format
- tmux control mode: `man 1 tmux` § "CONTROL MODE"
- Cloudflare Access: https://developers.cloudflare.com/cloudflare-one/applications/
- Cloudflare Tunnel: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/
- UnifiedPush: https://unifiedpush.org/
- ntfy: https://ntfy.sh/ and https://docs.ntfy.sh/
- Termux terminal modules (Apache 2.0): https://github.com/termux/termux-app/tree/master/terminal-emulator and `terminal-view`
- libsodium / NaCl: https://doc.libsodium.org/
- Lazysodium-Android: https://github.com/terl/lazysodium-android
- JSON Schema Draft 2020-12: https://json-schema.org/draft/2020-12/schema
- Conventional Commits: https://www.conventionalcommits.org/
- Architecture Decision Records: https://adr.github.io/

---

*End of spec v1.1.*
