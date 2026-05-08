// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/pty_resize.json

/**
 * Coordinator -> Daemon (originating from a client). Structured pane resize. See SPEC.md § 14.5.
 */
export interface PtyResize {
  type: "pty_resize";
  v: 1;
  session_id: string;
  rows: number;
  cols: number;
}
