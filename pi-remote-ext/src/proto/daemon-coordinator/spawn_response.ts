// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/spawn_response.json

/**
 * Daemon -> Coordinator (and relayed Coordinator -> App). Reply to a spawn_request. See SPEC.md §§ 10.2, 10.3, 15.
 */
export type SpawnResponse = {
  [k: string]: unknown;
} & {
  type: "spawn_response";
  v: 1;
  request_id: string;
  success: boolean;
  session_id?: string;
  tmux_target?: string;
  error?: string;
  /**
   * Set by the coordinator when relaying so only the originating client receives the message.
   */
  client_id?: string;
};
