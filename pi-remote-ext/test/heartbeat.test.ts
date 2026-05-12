// SPDX-License-Identifier: MIT
import { EventEmitter } from "node:events";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { startHeartbeat } from "../src/heartbeat.js";

class StubSocket extends EventEmitter {
  sent: unknown[] = [];
  send(msg: unknown): void {
    this.sent.push(msg);
  }
}

describe("startHeartbeat", () => {
  let socket: StubSocket;

  beforeEach(() => {
    vi.useFakeTimers();
    socket = new StubSocket();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("sends a heartbeat every 10s", () => {
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    const stop = startHeartbeat(socket);
    expect(socket.sent).toHaveLength(0);

    vi.advanceTimersByTime(10_000);
    expect(socket.sent).toHaveLength(1);
    expect(socket.sent[0]).toEqual({
      type: "heartbeat",
      ts: new Date("2026-01-01T00:00:10Z").getTime(),
    });

    vi.advanceTimersByTime(10_000);
    vi.advanceTimersByTime(10_000);
    expect(socket.sent).toHaveLength(3);
    stop();
  });

  it("stops sending heartbeats after stop() is called", () => {
    const stop = startHeartbeat(socket);
    vi.advanceTimersByTime(10_000);
    expect(socket.sent).toHaveLength(1);
    stop();
    vi.advanceTimersByTime(60_000);
    expect(socket.sent).toHaveLength(1);
  });

  it("stops automatically when the socket emits 'disconnected'", () => {
    startHeartbeat(socket);
    vi.advanceTimersByTime(10_000);
    expect(socket.sent).toHaveLength(1);

    socket.emit("disconnected");
    vi.advanceTimersByTime(60_000);
    expect(socket.sent).toHaveLength(1);
  });

  it("resumes when the socket emits 'reconnected'", () => {
    startHeartbeat(socket);
    socket.emit("disconnected");
    vi.advanceTimersByTime(30_000);
    expect(socket.sent).toHaveLength(0);

    socket.emit("reconnected");
    vi.advanceTimersByTime(10_000);
    expect(socket.sent).toHaveLength(1);
  });

  it("calls stop() exactly once even if invoked multiple times", () => {
    const stop = startHeartbeat(socket);
    vi.advanceTimersByTime(10_000);
    stop();
    stop();
    vi.advanceTimersByTime(60_000);
    expect(socket.sent).toHaveLength(1);
  });

  it("supports a custom cadence for tests", () => {
    startHeartbeat(socket, { intervalMs: 1000 });
    vi.advanceTimersByTime(2500);
    expect(socket.sent).toHaveLength(2);
  });
});
