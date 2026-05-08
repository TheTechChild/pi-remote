// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/session_ended.json

/**
 * Coordinator -> App. Forwarded session_ended that includes machine_id. See SPEC.md § 10.3.
 */
export interface SessionEnded {
  type: "session_ended";
  v: 1;
  session_id: string;
  machine_id: string;
  reason: "process_exit" | "extension_disconnect" | "tmux_server_lost" | "killed" | "spawn_failed";
}
