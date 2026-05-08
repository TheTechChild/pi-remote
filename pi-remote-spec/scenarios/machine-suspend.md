# Scenario: machine suspend

The MacBook lid closes mid-session. Sessions transition to `paused`. When the
lid reopens, the daemon reconnects and sessions resume their previous state.

## Pre-conditions

- Daemon is connected to coordinator.
- One Pi session is in state `running` and has at least one attached client.

## Walk-through

1. macOS posts `NSWorkspaceWillSleepNotification`. The daemon's suspend
   subsystem (see ADR D3) reacts.
2. Daemon sends
   [`machine_suspending`](../protocol/daemon-coordinator/machine_suspending.json)
   and closes its WebSocket gracefully (see fixture
   [`fixtures/daemon-coordinator-handshake.jsonl`](../fixtures/daemon-coordinator-handshake.jsonl)
   final record).
3. macOS suspends the daemon and tmux server processes.
4. Coordinator marks the machine as `suspended`, transitions all sessions on
   that machine to `paused`, and broadcasts
   [`machine_state_change`](../protocol/coordinator-app/machine_state_change.json)
   and per-session
   [`session_state_change`](../protocol/coordinator-app/session_state_change.json)
   to subscribed clients.
5. Phone shows paused sessions with the pause icon and grayed-out terminal
   view; input is rejected client-side.
6. Lid reopens. macOS resumes processes. The daemon's coordinator WebSocket
   is dead; the reconnect routine connects, sends `machine_register`, then
   for each still-live session sends
   [`session_resume`](../protocol/daemon-coordinator/session_resume.json) with
   `last_seq_emitted` from the daemon's in-memory counter (SPEC.md § 7.8).
7. Coordinator transitions the machine back to `online`, restores each
   session's pre-suspend state, and broadcasts the deltas.
8. Phone re-enables interaction.

## Post-conditions

- No push notifications fire from the suspended machine while it is offline
  (SPEC.md § 17).
- Other machines (e.g., the desktop) are unaffected.
- Pty data emitted while the daemon was suspended is, by definition, not
  produced — the OS paused the process. Nothing to backfill.
