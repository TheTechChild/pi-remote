// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/extension-daemon/ping.json

/**
 * Daemon -> Extension. Optional liveness ping. See SPEC.md § 10.1.
 */
export interface Ping {
  type: "ping";
  /**
   * Unix epoch milliseconds.
   */
  ts: number;
}
