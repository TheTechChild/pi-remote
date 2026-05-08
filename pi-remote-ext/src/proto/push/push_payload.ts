// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/push/push_payload.json

/**
 * Plaintext (pre-crypto_box) push payload sent from coordinator to phone via ntfy. See SPEC.md § 10.4.
 */
export interface PushPayload {
  v: 1;
  kind: "needs_attention";
  machine_id: string;
  machine_display_name: string;
  session_id: string;
  project_name: string;
  project_display_name?: string | null;
  reason:
    | "agent_idle"
    | "extension_dialog"
    | "tool_failure"
    | "queue_update"
    | "extension_error"
    | "compaction_complete"
    | "machine_suspended"
    | "session_ended"
    | "unresponsive";
  summary: string;
  ts: number;
  deep_link: string;
}
