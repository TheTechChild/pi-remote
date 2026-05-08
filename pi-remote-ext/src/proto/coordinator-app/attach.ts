// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/attach.json

/**
 * App -> Coordinator. Attach to a session and request replay starting after `last_seq`. See SPEC.md §§ 10.3, 18.4.
 */
export interface Attach {
  type: "attach";
  v: 1;
  session_id: string;
  /**
   * 0 means stream all available history then go live.
   */
  last_seq?: number;
}
