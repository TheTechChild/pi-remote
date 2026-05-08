// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/machine_suspending.json

/**
 * Daemon -> Coordinator. Sent immediately before the OS suspends the machine. See SPEC.md §§ 10.2, 17.
 */
export interface MachineSuspending {
  type: "machine_suspending";
  v: 1;
  ts: number;
}
