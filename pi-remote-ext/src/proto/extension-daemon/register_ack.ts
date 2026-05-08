// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/extension-daemon/register_ack.json

/**
 * Daemon -> Extension. Reply to a `register`. See SPEC.md § 10.1.
 */
export interface RegisterAck {
  type: "register_ack";
  v: 1;
  session_id: string;
  accepted: boolean;
  /**
   * Present and informative when `accepted` is false.
   */
  reason?: string;
}
