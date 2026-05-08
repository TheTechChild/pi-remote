// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/session_state_change.json

export type State = "running" | "idle" | "paused" | "unresponsive" | "ended";

/**
 * Daemon -> Coordinator (also forwarded to App). Session state transition. See SPEC.md §§ 10.2, 10.3, 12.
 */
export interface SessionStateChange {
  type: "session_state_change";
  v: 1;
  session_id: string;
  seq: number;
  ts: number;
  from: State;
  to: State;
}
