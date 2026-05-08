// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/machine_resumed.json

/**
 * Daemon -> Coordinator. Sent on the first reconnect after wake-from-suspend. See SPEC.md §§ 7.7, 17.
 */
export interface MachineResumed {
  type: "machine_resumed";
  v: 1;
  ts: number;
}
