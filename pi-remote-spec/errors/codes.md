# Error codes

This catalog enumerates the named error codes that may appear in the protocol's
error-bearing messages (`register_ack` with `accepted: false`, `spawn_response`
with `success: false`, internal coordinator and daemon log lines). Format:
`ERR_<COMPONENT>_<CONDITION>`. Codes are stable identifiers — phrasing of the
human-readable `reason`/`error` string may evolve, but the code does not.

| Code | Component | Description | Returned in |
|------|-----------|-------------|-------------|
| `ERR_DAEMON_DUPLICATE_SESSION_ID` | Daemon | A `register` arrived with a `session_id` already known to the daemon for a different pid. | `register_ack` |
| `ERR_DAEMON_SPAWN_TOKEN_MISMATCH` | Daemon | A `register` arrived with a spawn token that does not match any pending spawn request. | `register_ack` |
| `ERR_DAEMON_SPAWN_TOKEN_EXPIRED` | Daemon | The spawn token's 30-second window (D6) elapsed before the extension registered. | `register_ack`, `spawn_response` |
| `ERR_DAEMON_TMUX_UNAVAILABLE` | Daemon | The control-mode tmux client is not connected or has dropped (D7). | `spawn_response` |
| `ERR_DAEMON_TMUX_SPAWN_FAILED` | Daemon | tmux returned an error from `new-session` (e.g., invalid cwd, missing binary). | `spawn_response` |
| `ERR_DAEMON_PI_DID_NOT_REGISTER` | Daemon | tmux started a Pi process but no extension `register` arrived within 10s (§ 14.3). | `spawn_response` |
| `ERR_DAEMON_TMUX_SERVER_LOST` | Daemon | The control-mode connection closed (`%exit` or stdin EOF). | `session_ended.reason = tmux_server_lost` |
| `ERR_COORD_MACHINE_OFFLINE` | Coordinator | The named `machine_id` has no connected daemon. | `spawn_response` (synthetic, sent without daemon round-trip) |
| `ERR_COORD_SESSION_UNKNOWN` | Coordinator | The named `session_id` is not in the registry. | `spawn_response`, `attach`-flow error close |
| `ERR_COORD_REPLAY_UNAVAILABLE` | Coordinator | The requested `last_seq` is older than the ring's earliest entry. | `replay_unavailable` |
| `ERR_COORD_AUTH_REQUIRED` | Coordinator | WebSocket upgrade attempted without a valid CF Access JWT (clients) or service token (daemons). | HTTP 401/403 close frame |
| `ERR_COORD_PUSH_DISPATCH_FAILED` | Coordinator | ntfy returned a non-2xx response when posting an encrypted push payload. | Server log only (no protocol message) |
| `ERR_PUSH_DECRYPT_FAILED` | App | `crypto_box_open` returned an error. | Phone log + dropped notification |

The list is comprehensive for v1 surface area. Future codes are added through
ADRs and a follow-up PR to this file. Do not introduce a new code without an
ADR.
