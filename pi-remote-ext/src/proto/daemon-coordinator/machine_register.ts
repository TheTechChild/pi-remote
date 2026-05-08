// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/daemon-coordinator/machine_register.json

/**
 * Daemon -> Coordinator. First message after WebSocket open. See SPEC.md § 10.2.
 */
export interface MachineRegister {
  type: "machine_register";
  v: 1;
  machine_id: string;
  machine_display_name: string;
  daemon_version: string;
  capabilities: ("spawn" | "mirror")[];
}
