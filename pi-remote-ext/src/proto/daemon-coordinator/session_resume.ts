// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/session_resume.json

/**
 * Daemon -> Coordinator. Sent on reconnect for each still-live Pi session. Carries the daemon's last emitted seq so the broker can compute backfill availability. See SPEC.md §§ 7.8, 10.2.
 */
export interface SessionResume {
  type: "session_resume";
  v: 1;
  session_id: string;
  metadata: {
    [k: string]: unknown;
  };
  last_seq_emitted: number;
}
