# Scenario: attention notification

The agent reaches a point that requires user input — for example, an extension
dialog asking to run a destructive command — and the user is not currently
viewing the session in the app. The phone receives a push notification, the
user taps it, and the app deep-links into the relevant session.

## Pre-conditions

- Daemon is connected to coordinator.
- App has registered with the coordinator and provisioned a UnifiedPush
  endpoint with its X25519 public key.
- App is in the background or fully closed.

## Walk-through

1. Pi raises an `extension_ui_request` (or another dialog open).
2. The extension projects this to the daemon as
   `event.kind = "attention_dialog"` (see fixture
   [`fixtures/extension-register-flow.jsonl`](../fixtures/extension-register-flow.jsonl)
   for the broader register/event/disconnect skeleton).
3. The daemon forwards a `session_event` to the coordinator with the same kind.
4. The coordinator's broker:
   - Appends the event to the session's ring buffer.
   - Looks at every push-eligible client. For each client whose latest
     `client_focus` for this session is **not** `focused: true`, the
     coordinator builds the plaintext push payload conforming to
     [`protocol/push/push_payload.json`](../protocol/push/push_payload.json) —
     see fixture
     [`fixtures/push-payload-plaintext.json`](../fixtures/push-payload-plaintext.json).
5. The coordinator encrypts the payload with `crypto_box_easy` using the
   client's public key and the coordinator's secret key, then POSTs the
   ciphertext (`nonce || ct || mac`) to the client's UnifiedPush endpoint.
6. ntfy delivers the bytes to the ntfy distributor app on the phone.
7. The distributor broadcasts to `pi-remote-android`'s `BroadcastReceiver`,
   which decrypts and surfaces an Android notification on channel `attention`.
8. The user taps the notification. The deep link
   `pi-remote://session/<session_id>` opens MainActivity, which navigates to
   the terminal view, attaches via the
   [`attach`](../protocol/coordinator-app/attach.json) message, and replays
   from `last_seq = 0`.

## Post-conditions

- Push payload is end-to-end encrypted; ntfy and Cloudflare see only
  ciphertext and the topic path.
- The same scenario suppresses push if `client_focus.focused = true` for the
  session at dispatch time (SPEC.md § 8.7).
