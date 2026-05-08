// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/machine_state_change.json

/**
 * Coordinator -> App. Single-machine state delta. See SPEC.md § 10.3.
 */
export interface MachineStateChange {
  type: "machine_state_change";
  v: 1;
  machine_id: string;
  state: "online" | "suspended" | "offline";
}
