// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/spawn_response.json

/**
 * Coordinator -> App. Relayed spawn_response delivered only to the originating client. See SPEC.md §§ 10.3, 15.
 */
export interface SpawnResponse {
  type: "spawn_response";
  v: 1;
  request_id: string;
  success: boolean;
  session_id?: string;
  error?: string;
}
