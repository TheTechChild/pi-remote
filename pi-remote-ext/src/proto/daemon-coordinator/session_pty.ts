// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/session_pty.json

/**
 * Daemon -> Coordinator (also Coordinator -> App). Pty bytes from a session. See SPEC.md §§ 10.2, 10.3, 24 (compression reserved).
 */
export interface SessionPty {
  type: "session_pty";
  v: 1;
  session_id: string;
  seq: number;
  ts: number;
  /**
   * Base64-encoded raw pty bytes.
   */
  bytes: string;
  /**
   * Reserved for v2 (zstd). v1 omits or sets to null.
   */
  compression?: null | "none" | "zstd";
}
