# Scenario: replay on reconnect

A client's WebSocket dropped. It reconnects and resumes from the last
sequence number it had seen.

## Pre-conditions

- Coordinator has a session ring with `earliest_available_seq = 100` and
  `latest_seq = 245`.
- Client has already seen up to `seq = 200` before the disconnect.

## Walk-through

1. Client reconnects, sends `client_hello` and (re-)`subscribe_machine_list`.
2. Client sends [`attach`](../protocol/coordinator-app/attach.json) with
   `last_seq = 200`.
3. Coordinator's broker checks the ring: `200 >= earliest_available_seq` and
   `200 <= latest_seq`. It iterates entries with `seq > 200`, streams each
   buffered `session_event` and `session_pty` frame in seq order, and then
   transitions the client to live (SPEC.md § 18.4).
4. From this point onward the client receives live broker output exactly as
   before the disconnect.

## Post-conditions

- The client never sees a gap in the seq sequence. If it later wants to
  detect gaps, it can observe that received `seq` is monotonic and equal to
  prior `seq + 1` (or, for `session_event`, the next un-skipped seq).
- Pty bytes prior to `seq = 200` are not retransmitted; the client retains
  them locally in its terminal emulator.
