// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/session_state_change.json

export type State = "running" | "idle" | "paused" | "unresponsive" | "ended";

/**
 * Coordinator -> App. Forwarded session state transition with machine_id added. See SPEC.md § 10.3.
 */
export interface SessionStateChange {
  type: "session_state_change";
  v: 1;
  session_id: string;
  machine_id: string;
  from: State;
  to: State;
}
