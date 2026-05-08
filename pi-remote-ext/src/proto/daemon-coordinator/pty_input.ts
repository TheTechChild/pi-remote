// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/pty_input.json

/**
 * Coordinator -> Daemon. Forwarded keystrokes from a client. See SPEC.md §§ 10.2, 14.4.
 */
export interface PtyInput {
  type: "pty_input";
  v: 1;
  session_id: string;
  client_id: string;
  bytes: string;
}
