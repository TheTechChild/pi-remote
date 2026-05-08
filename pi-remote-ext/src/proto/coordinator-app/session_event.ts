// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/coordinator-app/session_event.json

/**
 * Coordinator -> App. Forwarded session_event sent only to clients attached to the session. See SPEC.md § 10.3.
 */
export interface SessionEvent {
  type: "session_event";
  v: 1;
  session_id: string;
  seq: number;
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
  ts: number;
  data: {};
}
