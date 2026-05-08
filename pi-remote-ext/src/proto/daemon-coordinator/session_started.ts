// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/session_started.json

/**
 * Daemon -> Coordinator. Emitted when a Pi session registers with the daemon. The same shape is forwarded by the coordinator to clients. See SPEC.md §§ 10.2, 10.3.
 */
export interface SessionStarted {
  type: "session_started";
  v: 1;
  session_id: string;
  machine_id: string;
  metadata: {
    cwd: string;
    project_name: string;
    project_display_name?: string | null;
    hostname: string;
    model: string;
    started_at: number;
    spawn_token?: string | null;
  };
}
