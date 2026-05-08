// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/spawn_request.json

/**
 * Coordinator -> Daemon. Asks the daemon to spawn a new Pi session in tmux. See SPEC.md §§ 10.2, 14.3, 15.
 */
export interface SpawnRequest {
  type: "spawn_request";
  v: 1;
  request_id: string;
  cwd: string;
  project_override?: string | null;
}
