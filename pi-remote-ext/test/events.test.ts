// SPDX-License-Identifier: MIT
import { mkdtemp, rm } from "node:fs/promises";
import { type Server, type Socket, createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TOOL_FAILURE_ERROR_MAX_CHARS, project, projectors } from "../src/events.js";
import piRemoteExtensionFactory from "../src/index.js";
import type { Event } from "../src/proto/extension-daemon/event.js";

const FIXED_NOW_ISO = "2026-01-01T00:00:00Z";
const FIXED_NOW_MS = new Date(FIXED_NOW_ISO).getTime();

function assertIsEvent(value: unknown): asserts value is Event {
  expect(value).toBeTypeOf("object");
  expect(value).not.toBeNull();
  const v = value as Record<string, unknown>;
  expect(v.type).toBe("event");
  expect(v.v).toBe(1);
  expect(typeof v.kind).toBe("string");
  expect(typeof v.ts).toBe("number");
  expect(v.data).toBeTypeOf("object");
  expect(v.data).not.toBeNull();
}

describe("events.ts — pure projectors", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(FIXED_NOW_ISO));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("agent_start → event{kind:'agent_start', data:{}}", () => {
    const out = projectors.agent_start({ type: "agent_start" });
    assertIsEvent(out);
    expect(out).toEqual({
      type: "event",
      v: 1,
      kind: "agent_start",
      ts: FIXED_NOW_MS,
      data: {},
    });
  });

  it("agent_end with messages → kind:'agent_end' with messages_summary", () => {
    const out = projectors.agent_end({
      type: "agent_end",
      messages: [
        { role: "user", content: "hi" },
        { role: "assistant", content: "done. tests pass." },
        {
          role: "assistant",
          content: "",
          tool_calls: [{ name: "read" }, { name: "edit" }],
        },
      ],
    });
    assertIsEvent(out);
    expect(out.kind).toBe("agent_end");
    expect(out.data).toEqual({
      messages_summary: {
        count: 3,
        last_text: "done. tests pass.",
        tools_used: ["read", "edit"],
      },
    });
  });

  it("agent_end with no messages → messages_summary{count:0,last_text:null,tools_used:[]}", () => {
    const out = projectors.agent_end({ type: "agent_end", messages: [] });
    assertIsEvent(out);
    expect(out.data).toEqual({
      messages_summary: { count: 0, last_text: null, tools_used: [] },
    });
  });

  it("extension_ui_request → kind:'attention_dialog'", () => {
    const out = projectors.extension_ui_request({
      type: "extension_ui_request",
      method: "prompt",
      title: "Continue?",
      options: ["yes", "no"],
    });
    assertIsEvent(out);
    expect(out.kind).toBe("attention_dialog");
    expect(out.data).toEqual({
      method: "prompt",
      title: "Continue?",
      options: ["yes", "no"],
    });
  });

  it("tool_execution_end with isError:true → kind:'tool_failure'", () => {
    const out = projectors.tool_execution_end({
      type: "tool_execution_end",
      toolName: "bash",
      isError: true,
      result: "command failed: exit 1",
    });
    assertIsEvent(out);
    expect(out.kind).toBe("tool_failure");
    expect(out.data).toEqual({
      toolName: "bash",
      error: "command failed: exit 1",
    });
  });

  it("tool_execution_end truncates long error excerpts", () => {
    const longError = "x".repeat(TOOL_FAILURE_ERROR_MAX_CHARS + 100);
    const out = projectors.tool_execution_end({
      type: "tool_execution_end",
      toolName: "bash",
      isError: true,
      result: longError,
    });
    assertIsEvent(out);
    const data = out.data as { error: string };
    expect(data.error.length).toBe(TOOL_FAILURE_ERROR_MAX_CHARS);
    expect(data.error).toBe("x".repeat(TOOL_FAILURE_ERROR_MAX_CHARS));
  });

  it("tool_execution_end with isError:false → null (no emit)", () => {
    const out = projectors.tool_execution_end({
      type: "tool_execution_end",
      toolName: "bash",
      isError: false,
      result: "ok",
    });
    expect(out).toBeNull();
  });

  it("queue_update → kind:'queue_update' with data.pending", () => {
    const out = projectors.queue_update({ type: "queue_update", pending: 3 });
    assertIsEvent(out);
    expect(out.kind).toBe("queue_update");
    expect(out.data).toEqual({ pending: 3 });
  });

  it("model_select → kind:'model_select' with data.model (string id)", () => {
    const out = projectors.model_select({
      type: "model_select",
      model: { id: "anthropic/claude-sonnet-4-20250514" },
    });
    assertIsEvent(out);
    expect(out.kind).toBe("model_select");
    expect(out.data).toEqual({ model: "anthropic/claude-sonnet-4-20250514" });
  });

  it("model_select accepts a bare string model id", () => {
    const out = projectors.model_select({
      type: "model_select",
      model: "anthropic/claude-sonnet-4-20250514",
    });
    assertIsEvent(out);
    expect(out.data).toEqual({ model: "anthropic/claude-sonnet-4-20250514" });
  });

  it("compaction_start → kind:'compaction_start'", () => {
    const out = projectors.compaction_start({ type: "compaction_start" });
    assertIsEvent(out);
    expect(out).toEqual({
      type: "event",
      v: 1,
      kind: "compaction_start",
      ts: FIXED_NOW_MS,
      data: {},
    });
  });

  it("compaction_end → kind:'compaction_end'", () => {
    const out = projectors.compaction_end({ type: "compaction_end" });
    assertIsEvent(out);
    expect(out).toEqual({
      type: "event",
      v: 1,
      kind: "compaction_end",
      ts: FIXED_NOW_MS,
      data: {},
    });
  });

  it("extension_error → kind:'extension_error' with data.message", () => {
    const out = projectors.extension_error({
      type: "extension_error",
      error: "boom",
      stack: "at foo (bar:1:1)",
    });
    assertIsEvent(out);
    expect(out.kind).toBe("extension_error");
    expect(out.data).toEqual({ message: "boom" });
    // stack is intentionally not on the wire for v1
    expect((out.data as Record<string, unknown>).stack).toBeUndefined();
  });

  it("project(name, payload) dispatches via the table", () => {
    const out = project("agent_start", { type: "agent_start" });
    assertIsEvent(out);
    expect(out.kind).toBe("agent_start");
  });

  it("project() returns null for the no-emit case", () => {
    const out = project("tool_execution_end", {
      type: "tool_execution_end",
      toolName: "x",
      isError: false,
      result: "",
    });
    expect(out).toBeNull();
  });

  it("ts equals Date.now() at projection time (fake-timer driven)", () => {
    const first = projectors.agent_start({ type: "agent_start" });
    assertIsEvent(first);
    vi.advanceTimersByTime(5000);
    const second = projectors.agent_start({ type: "agent_start" });
    assertIsEvent(second);
    expect(second.ts).toBe(first.ts + 5000);
  });
});

// ---------------------------------------------------------------------------
// Wiring tests: prove that index.ts hooks each Pi event, gates on register,
// and preserves projection order.
// ---------------------------------------------------------------------------

interface FakeDaemon {
  server: Server;
  path: string;
  connections: Socket[];
  frames: unknown[][];
  ackDelayMs?: number;
  ackResolvers: Array<() => void>;
  close: () => Promise<void>;
}

async function startFakeDaemon(socketPath: string): Promise<FakeDaemon> {
  const fake: FakeDaemon = {
    // biome-ignore lint/style/noNonNullAssertion: assigned below
    server: undefined!,
    path: socketPath,
    connections: [],
    frames: [],
    ackResolvers: [],
    close: async () => {
      await new Promise<void>((resolve) => fake.server.close(() => resolve()));
    },
  };
  fake.server = createServer((conn) => {
    const idx = fake.connections.length;
    fake.connections.push(conn);
    fake.frames.push([]);
    let buf = "";
    conn.on("data", (chunk) => {
      buf += chunk.toString("utf8");
      let nl = buf.indexOf("\n");
      while (nl >= 0) {
        const line = buf.slice(0, nl);
        buf = buf.slice(nl + 1);
        if (line.length > 0) {
          let parsed: unknown;
          try {
            parsed = JSON.parse(line);
          } catch {
            parsed = line;
          }
          fake.frames[idx]?.push(parsed);
          maybeAck(conn, parsed);
        }
        nl = buf.indexOf("\n");
      }
    });
  });

  function sendAck(conn: Socket, sessionId: unknown): void {
    if (conn.destroyed) return;
    const ack = {
      type: "register_ack",
      v: 1,
      session_id: sessionId,
      accepted: true,
    };
    conn.write(`${JSON.stringify(ack)}\n`);
  }

  function maybeAck(conn: Socket, parsed: unknown): void {
    if (typeof parsed !== "object" || parsed === null) return;
    const msg = parsed as Record<string, unknown>;
    if (msg.type !== "register") return;
    if (fake.ackDelayMs && fake.ackDelayMs > 0) {
      // Delay until manually released or timer fires.
      let released = false;
      const release = (): void => {
        if (released) return;
        released = true;
        sendAck(conn, msg.session_id);
      };
      fake.ackResolvers.push(release);
      setTimeout(release, fake.ackDelayMs);
    } else {
      sendAck(conn, msg.session_id);
    }
  }

  await new Promise<void>((resolve, reject) => {
    fake.server.once("error", reject);
    fake.server.listen(socketPath, () => {
      fake.server.removeListener("error", reject);
      resolve();
    });
  });
  return fake;
}

async function makeTempSocketPath(): Promise<{ path: string; cleanup: () => Promise<void> }> {
  const dir = await mkdtemp(join(tmpdir(), "pi-remote-ext-events-itest-"));
  return {
    path: join(dir, "daemon.sock"),
    cleanup: () => rm(dir, { recursive: true, force: true }),
  };
}

class FakeCtx {
  private handlers = new Map<string, (...args: unknown[]) => unknown>();
  logs: string[] = [];
  log = (msg: string): void => {
    this.logs.push(msg);
  };
  on = (event: string, handler: (...args: unknown[]) => unknown): void => {
    this.handlers.set(event, handler);
  };
  fire(event: string, ...args: unknown[]): unknown {
    return this.handlers.get(event)?.(...args);
  }
  has(event: string): boolean {
    return this.handlers.has(event);
  }
}

function frameKinds(frames: unknown[]): string[] {
  return frames
    .filter((f) => (f as Record<string, unknown>)?.type === "event")
    .map((f) => (f as Record<string, unknown>).kind as string);
}

async function waitUntilRegistered(ctx: FakeCtx): Promise<void> {
  await vi.waitFor(() => {
    expect(
      ctx.logs.some((l) => /^registered with daemon/.test(l)),
      `expected a 'registered with daemon' log; got: ${JSON.stringify(ctx.logs)}`,
    ).toBe(true);
  });
}

describe("events.ts — wiring through piRemoteExtensionFactory", () => {
  let cleanup: (() => Promise<void>) | undefined;
  let fake: FakeDaemon | undefined;

  beforeEach(() => {
    process.env.PI_REMOTE_SOCKET = "";
    process.env.PI_REMOTE_SPAWN_TOKEN = "";
  });

  afterEach(async () => {
    if (fake) {
      for (const c of fake.connections) c.destroy();
      await fake.close();
      fake = undefined;
    }
    await cleanup?.();
    cleanup = undefined;
    // biome-ignore lint/performance/noDelete: clearing PI_REMOTE_SOCKET / PI_REMOTE_SPAWN_TOKEN must not coerce undefined → "undefined".
    delete process.env.PI_REMOTE_SOCKET;
    // biome-ignore lint/performance/noDelete: see above.
    delete process.env.PI_REMOTE_SPAWN_TOKEN;
  });

  it("registers handlers for every Pi event in SPEC § 6.4", () => {
    const ctx = new FakeCtx();
    piRemoteExtensionFactory(ctx);
    for (const name of [
      "session_start",
      "session_shutdown",
      "agent_start",
      "agent_end",
      "extension_ui_request",
      "tool_execution_end",
      "queue_update",
      "model_select",
      "compaction_start",
      "compaction_end",
      "extension_error",
    ]) {
      expect(ctx.has(name), `expected ctx.on(${name}) to be registered`).toBe(true);
    }
  });

  it("drops events fired before register_ack with a debug log", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);
    fake.ackDelayMs = 10_000; // ack will be held until we manually release
    process.env.PI_REMOTE_SOCKET = t.path;

    const ctx = new FakeCtx();
    const inst = piRemoteExtensionFactory(ctx, { heartbeatIntervalMs: 1000 });
    // Fire session_start (do not await — register is in-flight).
    void ctx.fire("session_start");

    // Wait until the register frame has arrived at the daemon.
    await vi.waitFor(() => {
      expect(fake?.frames[0]?.length).toBeGreaterThanOrEqual(1);
      expect((fake?.frames[0]?.[0] as Record<string, unknown>).type).toBe("register");
    });

    // While ack is pending, fire some Pi events. They must be dropped.
    ctx.fire("agent_start", { type: "agent_start" });
    ctx.fire("queue_update", { type: "queue_update", pending: 2 });

    // Give the event loop a tick.
    await new Promise<void>((resolve) => setImmediate(resolve));
    expect(frameKinds(fake.frames[0] ?? [])).toEqual([]);
    expect(ctx.logs.some((l) => /drop.*pre[- ]?register/i.test(l))).toBe(true);

    // Release the ack so shutdown can complete.
    for (const r of fake.ackResolvers) r();
    fake.ackResolvers = [];
    await inst.shutdown?.();
  });

  it("emits projected events to the daemon after register_ack", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);
    process.env.PI_REMOTE_SOCKET = t.path;

    const ctx = new FakeCtx();
    const inst = piRemoteExtensionFactory(ctx, { heartbeatIntervalMs: 10_000 });
    await ctx.fire("session_start");

    // Wait for the register_ack round-trip to complete; index.ts logs
    // `registered with daemon (...)` immediately after the registered flag
    // flips. Using the log avoids firing test events as a probe (which
    // would race against later assertions about projection order).
    await waitUntilRegistered(ctx);

    ctx.fire("agent_start", { type: "agent_start" });

    await vi.waitFor(() => {
      expect(frameKinds(fake?.frames[0] ?? [])).toContain("agent_start");
    });

    await inst.shutdown?.();
  });

  it("preserves projection order under a synchronous burst", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);
    process.env.PI_REMOTE_SOCKET = t.path;

    const ctx = new FakeCtx();
    const inst = piRemoteExtensionFactory(ctx, { heartbeatIntervalMs: 10_000 });
    await ctx.fire("session_start");

    await waitUntilRegistered(ctx);

    // Synchronous burst, immediately after register: nothing else is in
    // flight from the test side, so any reordering would show up here.
    ctx.fire("agent_start", { type: "agent_start" });
    ctx.fire("queue_update", { type: "queue_update", pending: 1 });
    ctx.fire("queue_update", { type: "queue_update", pending: 2 });
    ctx.fire("agent_end", { type: "agent_end", messages: [] });

    await vi.waitFor(() => {
      expect(frameKinds(fake?.frames[0] ?? [])).toEqual([
        "agent_start",
        "queue_update",
        "queue_update",
        "agent_end",
      ]);
    });

    await inst.shutdown?.();
  });

  it("tool_execution_end with isError:false produces no event frame", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);
    process.env.PI_REMOTE_SOCKET = t.path;

    const ctx = new FakeCtx();
    const inst = piRemoteExtensionFactory(ctx, { heartbeatIntervalMs: 10_000 });
    await ctx.fire("session_start");

    await waitUntilRegistered(ctx);

    ctx.fire("tool_execution_end", {
      type: "tool_execution_end",
      toolName: "read",
      isError: false,
      result: "ok",
    });
    ctx.fire("tool_execution_end", {
      type: "tool_execution_end",
      toolName: "bash",
      isError: true,
      result: "boom",
    });

    await vi.waitFor(() => {
      expect(frameKinds(fake?.frames[0] ?? [])).toEqual(["tool_failure"]);
    });

    await inst.shutdown?.();
  });
});
