// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/session_pty.json

/**
 * Coordinator -> App. Forwarded session_pty sent only to attached clients. See SPEC.md § 10.3.
 */
export interface SessionPty {
  type: "session_pty";
  v: 1;
  session_id: string;
  seq: number;
  ts: number;
  bytes: string;
  compression?: null | "none" | "zstd";
}
