// SPDX-License-Identifier: MIT
import { describe, expect, it } from "vitest";
import { EVENTS_TABLE, PI_EVENT_NAMES, type PiEventName } from "../src/events-table.js";
import type { Event } from "../src/proto/extension-daemon/event.js";

// Mirror of the generated Event["kind"] union. If the schema/codegen adds or
// removes a kind, the type-level check below fails to compile, then the
// runtime set equality test below fails.
const KINDS: Array<Event["kind"]> = [
  "agent_start",
  "agent_end",
  "attention_dialog",
  "tool_failure",
  "queue_update",
  "model_select",
  "compaction_start",
  "compaction_end",
  "extension_error",
];

// Compile-time assertion: KINDS covers the full generated union.
type _KindsCoverUnion = Exclude<Event["kind"], (typeof KINDS)[number]> extends never ? true : never;
const _kindsCoverUnion: _KindsCoverUnion = true;
void _kindsCoverUnion;

// Per-Pi-event fixture payloads, valid enough that each projector returns a
// non-null Event (except tool_execution_end with isError:false, exercised
// separately).
const FIXTURES: Record<PiEventName, unknown> = {
  agent_start: { type: "agent_start" },
  agent_end: { type: "agent_end", messages: [] },
  extension_ui_request: {
    type: "extension_ui_request",
    method: "prompt",
    title: "Continue?",
    options: ["yes", "no"],
  },
  tool_execution_end: {
    type: "tool_execution_end",
    toolName: "bash",
    isError: true,
    result: "fail",
  },
  queue_update: { type: "queue_update", pending: 0 },
  model_select: { type: "model_select", model: { id: "anthropic/test" } },
  compaction_start: { type: "compaction_start" },
  compaction_end: { type: "compaction_end" },
  extension_error: { type: "extension_error", error: "boom" },
};

describe("events-table.ts — exhaustiveness", () => {
  it("EVENTS_TABLE has an entry for every Pi event name in SPEC § 6.4", () => {
    expect(Object.keys(EVENTS_TABLE).sort()).toEqual([...PI_EVENT_NAMES].sort());
  });

  it("PI_EVENT_NAMES matches SPEC § 6.4 exactly (minus session_start/session_shutdown)", () => {
    expect([...PI_EVENT_NAMES].sort()).toEqual(
      [
        "agent_start",
        "agent_end",
        "extension_ui_request",
        "tool_execution_end",
        "queue_update",
        "model_select",
        "compaction_start",
        "compaction_end",
        "extension_error",
      ].sort(),
    );
  });

  it("every literal in Event['kind'] is produced by at least one projector", () => {
    const emitted = new Set<string>();
    for (const name of PI_EVENT_NAMES) {
      const out = EVENTS_TABLE[name](FIXTURES[name]);
      if (out) emitted.add(out.kind);
    }
    expect([...emitted].sort()).toEqual([...KINDS].sort());
  });

  it("only tool_execution_end may return null; all other entries emit", () => {
    for (const name of PI_EVENT_NAMES) {
      const out = EVENTS_TABLE[name](FIXTURES[name]);
      if (name === "tool_execution_end") {
        // The fixture for tool_execution_end has isError:true, so it must emit.
        expect(out, "tool_execution_end with isError:true must emit").not.toBeNull();
        continue;
      }
      expect(out, `${name} must always emit a frame for a valid payload`).not.toBeNull();
    }
  });

  it("table entries are pure (same input → structurally equal output, locked ts)", () => {
    // No fake timers needed: we just ensure that for inputs without time
    // dependencies, the projector's only time-varying field is `ts`. Lock
    // the clock by calling rapidly and tolerating up to a few ms drift, OR
    // simpler: compare everything except `ts`.
    for (const name of PI_EVENT_NAMES) {
      const a = EVENTS_TABLE[name](FIXTURES[name]);
      const b = EVENTS_TABLE[name](FIXTURES[name]);
      if (a === null || b === null) {
        expect(a).toBe(b);
        continue;
      }
      const stripTs = (e: Event): Omit<Event, "ts"> => {
        const { ts: _ts, ...rest } = e;
        return rest;
      };
      expect(stripTs(a)).toEqual(stripTs(b));
    }
  });
});
