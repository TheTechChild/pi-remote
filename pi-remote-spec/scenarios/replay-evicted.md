# Scenario: replay evicted

The client was disconnected long enough — or output was heavy enough — that
the broker has evicted entries past the client's known `last_seq`. The
coordinator answers honestly and the client transitions to live without
backfill.

## Pre-conditions

- Coordinator broker ring for the target session has
  `earliest_available_seq = 300`, `latest_seq = 500`.
- Client last saw `seq = 200`.

## Walk-through

1. Client `attach`es with `last_seq = 200`.
2. Broker observes `200 < earliest_available_seq (300)` and replies with
   [`replay_unavailable`](../protocol/coordinator-app/replay_unavailable.json)
   carrying `earliest_available_seq = 300` and `current_seq = 500`.
3. Client transitions immediately to live and renders subsequent
   `session_event`/`session_pty` frames.
4. The phone UI surfaces the SPEC.md § 8.6 empty-state copy:
   > "Even though no output is showing up, the session is connected. As you
   > type and `<machine name>` responds, you will see new output here."
5. User can still interact; the gap in pty history is cosmetic.

## Post-conditions

- No partial backfill is sent — replay is all-or-nothing relative to the ring
  contents (SPEC.md § 18.4 case 3).
- Source of truth for the missed bytes is Pi's own JSONL session file on the
  coding machine. The phone does not reconstruct it (non-goal N8).
