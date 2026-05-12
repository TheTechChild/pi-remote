// SPDX-License-Identifier: MIT
import { EventEmitter } from "node:events";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Register } from "../src/proto/extension-daemon/register.js";
import { buildRegister, registerWithDaemon } from "../src/register.js";

class StubSocket extends EventEmitter {
  sent: unknown[] = [];
  send(msg: unknown): void {
    this.sent.push(msg);
  }
}

describe("buildRegister", () => {
  const ORIGINAL_TOKEN = process.env.PI_REMOTE_SPAWN_TOKEN;

  afterEach(() => {
    // biome-ignore lint/performance/noDelete: process.env coerces undefined to the string "undefined"; delete is the only correct way to clear a var.
    if (ORIGINAL_TOKEN === undefined) delete process.env.PI_REMOTE_SPAWN_TOKEN;
    else process.env.PI_REMOTE_SPAWN_TOKEN = ORIGINAL_TOKEN;
  });

  it("produces a valid Register payload from a session context", () => {
    // biome-ignore lint/performance/noDelete: see afterEach.
    delete process.env.PI_REMOTE_SPAWN_TOKEN;
    const reg = buildRegister({
      sessionId: "session-uuid-1",
      cwd: "/home/clayton/projects/foo",
      projectName: "foo",
      projectDisplayName: null,
      tmuxTarget: "untmuxed:0.0",
      pid: 12345,
      hostname: "macbook.local",
      model: "anthropic/claude-sonnet-4-20250514",
      startedAt: 1730000000000,
    });
    expect(reg).toEqual<Register>({
      type: "register",
      v: 1,
      session_id: "session-uuid-1",
      spawn_token: null,
      cwd: "/home/clayton/projects/foo",
      project_name: "foo",
      project_display_name: null,
      tmux_target: "untmuxed:0.0",
      pid: 12345,
      hostname: "macbook.local",
      model: "anthropic/claude-sonnet-4-20250514",
      started_at: 1730000000000,
    });
  });

  it("includes spawn_token when PI_REMOTE_SPAWN_TOKEN is set", () => {
    process.env.PI_REMOTE_SPAWN_TOKEN = "abc123def456";
    const reg = buildRegister({
      sessionId: "s1",
      cwd: "/a",
      projectName: "a",
      projectDisplayName: null,
      tmuxTarget: "untmuxed:0.0",
      pid: 1,
      hostname: "h",
      model: "m",
      startedAt: 0,
    });
    expect(reg.spawn_token).toBe("abc123def456");
  });

  it("emits spawn_token=null when PI_REMOTE_SPAWN_TOKEN is unset", () => {
    // biome-ignore lint/performance/noDelete: see afterEach.
    delete process.env.PI_REMOTE_SPAWN_TOKEN;
    const reg = buildRegister({
      sessionId: "s1",
      cwd: "/a",
      projectName: "a",
      projectDisplayName: null,
      tmuxTarget: "untmuxed:0.0",
      pid: 1,
      hostname: "h",
      model: "m",
      startedAt: 0,
    });
    expect(reg.spawn_token).toBeNull();
  });
});

describe("registerWithDaemon", () => {
  let socket: StubSocket;
  const payload: Register = {
    type: "register",
    v: 1,
    session_id: "s1",
    spawn_token: null,
    cwd: "/cwd",
    project_name: "p",
    project_display_name: null,
    tmux_target: "untmuxed:0.0",
    pid: 1,
    hostname: "h",
    model: "m",
    started_at: 0,
  };

  beforeEach(() => {
    socket = new StubSocket();
  });

  it("sends the register message and resolves on accepted=true", async () => {
    const promise = registerWithDaemon(socket, payload);
    expect(socket.sent[0]).toEqual(payload);
    socket.emit("message", {
      type: "register_ack",
      v: 1,
      session_id: "s1",
      accepted: true,
    });
    await expect(promise).resolves.toEqual({
      type: "register_ack",
      v: 1,
      session_id: "s1",
      accepted: true,
    });
  });

  it("rejects with the daemon's reason on accepted=false", async () => {
    const promise = registerWithDaemon(socket, payload);
    socket.emit("message", {
      type: "register_ack",
      v: 1,
      session_id: "s1",
      accepted: false,
      reason: "duplicate session_id",
    });
    await expect(promise).rejects.toThrow(/duplicate session_id/);
  });

  it("ignores non-register_ack messages while waiting", async () => {
    const promise = registerWithDaemon(socket, payload);
    socket.emit("message", { type: "ping", ts: 1 });
    socket.emit("message", {
      type: "register_ack",
      v: 1,
      session_id: "s1",
      accepted: true,
    });
    await expect(promise).resolves.toMatchObject({ accepted: true });
  });

  it("rejects on timeout if no register_ack arrives", async () => {
    vi.useFakeTimers();
    const promise = registerWithDaemon(socket, payload, { timeoutMs: 5000 });
    // Catch the eventual rejection up-front so the unhandled-rejection
    // tracker doesn't fire while we're advancing timers.
    const guarded = promise.catch((err) => err);
    await vi.advanceTimersByTimeAsync(5001);
    const err = await guarded;
    expect(err).toBeInstanceOf(Error);
    expect((err as Error).message).toMatch(/register_ack/);
    vi.useRealTimers();
  });
});
