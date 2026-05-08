// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/session_ended.json

/**
 * Daemon -> Coordinator. Pi process ended. See SPEC.md §§ 10.2, 10.3.
 */
export interface SessionEnded {
  type: "session_ended";
  v: 1;
  session_id: string;
  seq: number;
  reason: "process_exit" | "extension_disconnect" | "tmux_server_lost" | "killed" | "spawn_failed";
}
