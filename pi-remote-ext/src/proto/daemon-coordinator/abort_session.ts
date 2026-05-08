// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/abort_session.json

/**
 * Coordinator -> Daemon. Terminate a Pi session. See SPEC.md § 10.2.
 */
export interface AbortSession {
  type: "abort_session";
  v: 1;
  session_id: string;
  mode: "kill";
}
