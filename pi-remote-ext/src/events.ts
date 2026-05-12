// SPDX-License-Identifier: MIT
//
// Pure projectors: Pi event payload -> wire `event` frame.
//
// Each projector is a pure function with no side effects and no reference to
// the daemon socket. The wiring layer in index.ts decides when (and whether)
// to forward the returned frame; see SPEC.md §§ 6.4 and 10.1.
//
// Adding a new Pi event:
//   1. Add a literal to `PI_EVENT_NAMES` in events-table.ts.
//   2. Add a projector below.
//   3. Reference it from EVENTS_TABLE.
//
// The exhaustiveness check in test/events-table.test.ts will fail to compile
// (and at runtime) until the new entry covers every literal in Event["kind"].

import type { Event } from "./proto/extension-daemon/event.js";

/**
 * Pure-projector signature. Accepts an opaque Pi payload; returns a wire
 * frame, or null to indicate "do not emit" (used by tool_execution_end when
 * isError is false).
 */
export type Projector = (piPayload: unknown) => Event | null;

/**
 * Maximum number of characters retained from `tool_execution_end.result` when
 * building the `tool_failure.error` excerpt. Longer values are truncated.
 * Documented so the test that asserts truncation has a stable bound.
 */
export const TOOL_FAILURE_ERROR_MAX_CHARS = 512;

function obj(v: unknown): Record<string, unknown> {
  return typeof v === "object" && v !== null ? (v as Record<string, unknown>) : {};
}

function nowMs(): number {
  return Date.now();
}

function frame<K extends Event["kind"]>(kind: K, data: Event["data"]): Event {
  return { type: "event", v: 1, kind, ts: nowMs(), data };
}

// --- Per-event projectors ---------------------------------------------------

export const projectAgentStart: Projector = (_payload) => frame("agent_start", {});

interface AgentMessageLike {
  role?: unknown;
  content?: unknown;
  tool_calls?: unknown;
}

function summarizeMessages(messages: AgentMessageLike[]): {
  count: number;
  last_text: string | null;
  tools_used: string[];
} {
  const count = messages.length;
  let last_text: string | null = null;
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (m && typeof m.content === "string" && m.content.length > 0) {
      last_text = m.content;
      break;
    }
  }
  const tools_used: string[] = [];
  const seen = new Set<string>();
  for (const m of messages) {
    if (!m || !Array.isArray(m.tool_calls)) continue;
    for (const tc of m.tool_calls as Array<Record<string, unknown>>) {
      const name = tc && typeof tc.name === "string" ? tc.name : null;
      if (name && !seen.has(name)) {
        seen.add(name);
        tools_used.push(name);
      }
    }
  }
  return { count, last_text, tools_used };
}

export const projectAgentEnd: Projector = (payload) => {
  const p = obj(payload);
  const messages = Array.isArray(p.messages) ? (p.messages as AgentMessageLike[]) : [];
  return frame("agent_end", { messages_summary: summarizeMessages(messages) });
};

export const projectExtensionUiRequest: Projector = (payload) => {
  const p = obj(payload);
  const method = typeof p.method === "string" ? p.method : "";
  const title = typeof p.title === "string" ? p.title : "";
  const options = Array.isArray(p.options) ? p.options : [];
  return frame("attention_dialog", { method, title, options });
};

export const projectToolExecutionEnd: Projector = (payload) => {
  const p = obj(payload);
  if (p.isError !== true) return null;
  const toolName = typeof p.toolName === "string" ? p.toolName : "";
  const raw = p.result;
  let error: string;
  if (typeof raw === "string") {
    error = raw;
  } else if (raw === undefined || raw === null) {
    error = "";
  } else {
    try {
      error = JSON.stringify(raw);
    } catch {
      error = String(raw);
    }
  }
  if (error.length > TOOL_FAILURE_ERROR_MAX_CHARS) {
    error = error.slice(0, TOOL_FAILURE_ERROR_MAX_CHARS);
  }
  return frame("tool_failure", { toolName, error });
};

export const projectQueueUpdate: Projector = (payload) => {
  const p = obj(payload);
  const pending = typeof p.pending === "number" ? p.pending : 0;
  return frame("queue_update", { pending });
};

export const projectModelSelect: Projector = (payload) => {
  const p = obj(payload);
  let model = "";
  if (typeof p.model === "string") {
    model = p.model;
  } else if (typeof p.model === "object" && p.model !== null) {
    const m = p.model as Record<string, unknown>;
    if (typeof m.id === "string") model = m.id;
  }
  return frame("model_select", { model });
};

export const projectCompactionStart: Projector = (_payload) => frame("compaction_start", {});
export const projectCompactionEnd: Projector = (_payload) => frame("compaction_end", {});

export const projectExtensionError: Projector = (payload) => {
  const p = obj(payload);
  const message =
    typeof p.error === "string" ? p.error : typeof p.message === "string" ? p.message : "";
  return frame("extension_error", { message });
};

/**
 * Per-Pi-event-name handle on the projector functions. Tests use this to
 * exercise each projector by name without going through the table.
 */
export const projectors = {
  agent_start: projectAgentStart,
  agent_end: projectAgentEnd,
  extension_ui_request: projectExtensionUiRequest,
  tool_execution_end: projectToolExecutionEnd,
  queue_update: projectQueueUpdate,
  model_select: projectModelSelect,
  compaction_start: projectCompactionStart,
  compaction_end: projectCompactionEnd,
  extension_error: projectExtensionError,
} as const;

// The dispatcher `project(name, payload)` lives in events-table.ts to avoid
// a circular import (events-table.ts already imports the projectors here).
// Re-export it through this module so callers have a single import surface.
export { project } from "./events-table.js";
