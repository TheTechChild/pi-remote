// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/detach.json

/**
 * App -> Coordinator. Explicit detach (close is also accepted). See SPEC.md §§ 10.3, 16.3.
 */
export interface Detach {
  type: "detach";
  v: 1;
  session_id: string;
}
