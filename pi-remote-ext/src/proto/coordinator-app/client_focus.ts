// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/client_focus.json

/**
 * App -> Coordinator. Reports whether the session is currently in the foreground for this client (used to suppress push). See SPEC.md §§ 9.7, 10.3.
 */
export interface ClientFocus {
  type: "client_focus";
  v: 1;
  session_id: string;
  focused: boolean;
}
