// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/machine_list.json

/**
 * Coordinator -> App. Initial list and subsequent full snapshots of machines and their sessions. See SPEC.md § 10.3.
 */
export interface MachineList {
  type: "machine_list";
  v: 1;
  machines: {
    machine_id: string;
    machine_display_name: string;
    state: "online" | "suspended" | "offline";
    sessions: {
      session_id: string;
      metadata: {};
      state: "running" | "idle" | "paused" | "unresponsive" | "ended";
    }[];
  }[];
}
