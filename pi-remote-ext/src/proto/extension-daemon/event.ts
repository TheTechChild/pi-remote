// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/extension-daemon/event.json

/**
 * Extension -> Daemon. Projection of a Pi event onto Pi Remote's protocol. See SPEC.md §§ 6.4, 10.1.
 */
export interface Event {
  type: "event";
  v: 1;
  kind:
    | "agent_start"
    | "agent_end"
    | "attention_dialog"
    | "tool_failure"
    | "queue_update"
    | "model_select"
    | "compaction_start"
    | "compaction_end"
    | "extension_error";
  /**
   * Unix epoch milliseconds.
   */
  ts: number;
  /**
   * Kind-specific payload. Free-form for v1; per-kind shapes are documented in SPEC.md § 6.4.
   */
  data: {};
}
