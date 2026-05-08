// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/spawn_session.json

/**
 * App -> Coordinator. Asks coordinator to relay a spawn request to the named machine. See SPEC.md §§ 10.3, 15.
 */
export interface SpawnSession {
  type: "spawn_session";
  v: 1;
  request_id: string;
  machine_id: string;
  cwd: string;
  project_override?: string | null;
}
