// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/pty_input.json

/**
 * App -> Coordinator. Keystrokes the user typed in the terminal view. See SPEC.md §§ 10.3, 14.4.
 */
export interface PtyInput {
  type: "pty_input";
  v: 1;
  session_id: string;
  bytes: string;
}
