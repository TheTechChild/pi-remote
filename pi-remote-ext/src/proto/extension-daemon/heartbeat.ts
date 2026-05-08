// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/extension-daemon/heartbeat.json

/**
 * Extension -> Daemon. 10-second cadence liveness ping. See SPEC.md §§ 6.6, 10.1.
 */
export interface Heartbeat {
  type: "heartbeat";
  /**
   * Unix epoch milliseconds.
   */
  ts: number;
}
