// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/extension-daemon/disconnect.json

/**
 * Extension -> Daemon. Sent on `session_shutdown`. See SPEC.md §§ 6.6, 10.1.
 */
export interface Disconnect {
  type: "disconnect";
  reason: "session_shutdown" | "client_request" | "error";
}
