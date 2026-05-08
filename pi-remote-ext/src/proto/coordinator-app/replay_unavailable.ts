// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/replay_unavailable.json

/**
 * Coordinator -> App. Sent in response to attach when the requested last_seq is older than the ring's earliest entry. See SPEC.md §§ 10.3, 18.4.
 */
export interface ReplayUnavailable {
  type: "replay_unavailable";
  v: 1;
  session_id: string;
  earliest_available_seq: number;
  current_seq: number;
}
