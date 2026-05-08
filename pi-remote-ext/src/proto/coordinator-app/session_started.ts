// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/session_started.json

/**
 * Coordinator -> App. Mirror of the daemon's session_started, broadcast to subscribed clients. See SPEC.md § 10.3.
 */
export interface SessionStarted {
  type: "session_started";
  v: 1;
  session_id: string;
  machine_id: string;
  metadata: {};
}
