// SPDX-License-Identifier: MIT
//
// Data-driven projector table. The `satisfies` constraint binds it to the
// generated wire `Event` type; the test in test/events-table.test.ts asserts
// every literal in `Event["kind"]` is covered by at least one entry.
//
// To add a new Pi event:
//   1. Add the Pi-side event name as a literal to PI_EVENT_NAMES below.
//   2. Add the projector to src/events.ts.
//   3. Wire it into EVENTS_TABLE.
//   4. Ensure index.ts registers `ctx.on(name, …)` for it (the unit test
//      "registers handlers for every Pi event in SPEC § 6.4" enforces this).

import {
  type Projector,
  projectAgentEnd,
  projectAgentStart,
  projectCompactionEnd,
  projectCompactionStart,
  projectExtensionError,
  projectExtensionUiRequest,
  projectModelSelect,
  projectQueueUpdate,
  projectToolExecutionEnd,
} from "./events.js";
import type { Event } from "./proto/extension-daemon/event.js";

/**
 * The Pi event names that map to wire `event` frames per SPEC.md § 6.4.
 *
 * `session_start` and `session_shutdown` are intentionally absent — they are
 * handshake / disconnect frames (`register` / `disconnect`), not `event`
 * projections, and are wired separately in index.ts.
 */
export const PI_EVENT_NAMES = [
  "agent_start",
  "agent_end",
  "extension_ui_request",
  "tool_execution_end",
  "queue_update",
  "model_select",
  "compaction_start",
  "compaction_end",
  "extension_error",
] as const;

export type PiEventName = (typeof PI_EVENT_NAMES)[number];

export const EVENTS_TABLE = {
  agent_start: projectAgentStart,
  agent_end: projectAgentEnd,
  extension_ui_request: projectExtensionUiRequest,
  tool_execution_end: projectToolExecutionEnd,
  queue_update: projectQueueUpdate,
  model_select: projectModelSelect,
  compaction_start: projectCompactionStart,
  compaction_end: projectCompactionEnd,
  extension_error: projectExtensionError,
} as const satisfies Record<PiEventName, Projector>;

/**
 * Convenience: dispatch by Pi event name. Returns the projected frame or
 * null when the projector elects not to emit (currently only
 * `tool_execution_end` with `isError:false`).
 */
export function project(name: PiEventName, payload: unknown): Event | null {
  return EVENTS_TABLE[name](payload);
}
