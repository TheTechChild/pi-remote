# Scenario: multi-attach

Two clients (the phone and the local terminal via `tmux a`) are attached to
the same Pi session. Both observe the full pty stream and either can send
input.

## Pre-conditions

- Pi session is running on `macbook-pro`.
- Phone has authenticated and received the `machine_list` snapshot
  (see fixture
  [`fixtures/client-attach-flow.jsonl`](../fixtures/client-attach-flow.jsonl)).
- A local user has run `tmux a -t pi-remote-<uuid>` on the MacBook.

## Walk-through

1. Phone sends `attach` with `last_seq = 0`. Coordinator streams the entire
   ring then transitions the client to live mode.
2. Pi emits output. The daemon receives the bytes via tmux `%output` and
   forwards `session_pty` (see fixture
   [`fixtures/daemon-coordinator-handshake.jsonl`](../fixtures/daemon-coordinator-handshake.jsonl)).
3. Coordinator broadcasts the `session_pty` frame to every attached client.
   Both the local user and the phone see the same bytes.
4. The phone user types `yes\n`. The app sends `pty_input` to the
   coordinator, which forwards a daemon-targeted
   [`pty_input`](../protocol/daemon-coordinator/pty_input.json) bearing
   `client_id`. The daemon converts this into `tmux send-keys` (SPEC.md
   § 14.4) so the keystroke is processed identically to local input.
5. tmux echoes the typed characters; both clients see the echo via subsequent
   `session_pty` frames.

## Post-conditions

- Last-write-wins; concurrent typing from both clients is interleaved at the
  tmux server in arrival order (SPEC.md § 16.1).
- Detach is implicit on WebSocket close; clients may also send the explicit
  [`detach`](../protocol/coordinator-app/detach.json) message.
