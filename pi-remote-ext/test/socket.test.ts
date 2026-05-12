// SPDX-License-Identifier: MIT
import { EventEmitter } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { type Server, type Socket, createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DaemonSocket } from "../src/socket.js";

interface FakeDaemon {
  server: Server;
  path: string;
  connections: Socket[];
  received: string[][];
  close: () => Promise<void>;
}

async function startFakeDaemon(socketPath: string): Promise<FakeDaemon> {
  const fake: FakeDaemon = {
    // biome-ignore lint/style/noNonNullAssertion: assigned below
    server: undefined!,
    path: socketPath,
    connections: [],
    received: [],
    close: async () => {
      await new Promise<void>((resolve) => fake.server.close(() => resolve()));
    },
  };
  fake.server = createServer((conn) => {
    const idx = fake.connections.length;
    fake.connections.push(conn);
    fake.received.push([]);
    let buf = "";
    conn.on("data", (chunk) => {
      buf += chunk.toString("utf8");
      let nl = buf.indexOf("\n");
      while (nl >= 0) {
        const line = buf.slice(0, nl);
        buf = buf.slice(nl + 1);
        if (line.length > 0) fake.received[idx]?.push(line);
        nl = buf.indexOf("\n");
      }
    });
  });
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
  const dir = await mkdtemp(join(tmpdir(), "pi-remote-ext-test-"));
  return {
    path: join(dir, "daemon.sock"),
    cleanup: () => rm(dir, { recursive: true, force: true }),
  };
}

describe("DaemonSocket", () => {
  let cleanup: (() => Promise<void>) | undefined;
  let fake: FakeDaemon | undefined;
  let socket: DaemonSocket | undefined;

  afterEach(async () => {
    socket?.close();
    socket = undefined;
    if (fake) {
      for (const c of fake.connections) c.destroy();
      await fake.close();
      fake = undefined;
    }
    await cleanup?.();
    cleanup = undefined;
    vi.useRealTimers();
  });

  it("connects when daemon socket is available", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);

    socket = new DaemonSocket({ path: t.path });
    await socket.connect();
    expect(socket.connected).toBe(true);
  });

  it("sends messages as newline-delimited JSON", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);

    socket = new DaemonSocket({ path: t.path });
    await socket.connect();
    socket.send({ type: "heartbeat", ts: 123 });
    socket.send({ type: "heartbeat", ts: 456 });

    // Wait until the fake daemon has seen both lines.
    await vi.waitFor(() => {
      expect(fake?.received[0]?.length).toBe(2);
    });
    expect(fake?.received[0]?.[0]).toBe('{"type":"heartbeat","ts":123}');
    expect(fake?.received[0]?.[1]).toBe('{"type":"heartbeat","ts":456}');
  });

  it("emits parsed messages received from the daemon as JSONL frames", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);

    socket = new DaemonSocket({ path: t.path });
    const messages: unknown[] = [];
    socket.on("message", (msg) => messages.push(msg));
    await socket.connect();

    // Wait for the daemon side to register the connection, then write two
    // frames (one of which arrives in two TCP-level chunks to exercise framing).
    await vi.waitFor(() => {
      expect(fake?.connections.length).toBe(1);
    });
    const conn = fake?.connections[0];
    if (!conn) throw new Error("no connection");
    conn.write('{"type":"register_ack","v":1,"session_id":"s1","accepted":true}\n');
    conn.write('{"type":"pi');
    conn.write('ng","ts":42}\n');

    await vi.waitFor(() => {
      expect(messages.length).toBe(2);
    });
    expect(messages[0]).toEqual({
      type: "register_ack",
      v: 1,
      session_id: "s1",
      accepted: true,
    });
    expect(messages[1]).toEqual({ type: "ping", ts: 42 });
  });

  it("returns connect-failure when no daemon is listening (no auto-reconnect)", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    // no fake daemon started → socket file does not exist

    socket = new DaemonSocket({ path: t.path, autoReconnect: false });
    await expect(socket.connect()).rejects.toThrow();
    expect(socket.connected).toBe(false);
  });

  it("uses exponential backoff (1s, 2s, 4s, … capped at 30s) on mid-session reconnect", async () => {
    vi.useFakeTimers();
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;

    // Inject a dial that succeeds the first time and fails every time after,
    // so we can observe the backoff schedule deterministically without real I/O.
    const fakeSocketRef: { current: EventEmitter | undefined } = { current: undefined };
    let attempts = 0;
    const dial = async (): Promise<unknown> => {
      attempts++;
      if (attempts === 1) {
        const s = new EventEmitter() as EventEmitter & {
          write: () => void;
          destroy: () => void;
          removeAllListeners: EventEmitter["removeAllListeners"];
        };
        s.write = () => {};
        s.destroy = () => {};
        fakeSocketRef.current = s;
        return s;
      }
      throw new Error("ECONNREFUSED");
    };

    const delays: number[] = [];
    socket = new DaemonSocket({
      path: t.path,
      autoReconnect: true,
      onScheduleReconnect: (delayMs) => delays.push(delayMs),
      // biome-ignore lint/suspicious/noExplicitAny: fake socket for timing test
      dial: dial as any,
    });
    await socket.connect();
    expect(socket.connected).toBe(true);

    // Drop the connection — this kicks off the backoff loop.
    fakeSocketRef.current?.emit("close");
    expect(socket.connected).toBe(false);

    // Advance the fake clock through several backoff windows.
    for (let i = 0; i < 8; i++) {
      await vi.advanceTimersByTimeAsync(60_000);
    }

    expect(delays.slice(0, 6)).toEqual([1000, 2000, 4000, 8000, 16000, 30000]);
    expect(delays.every((d) => d <= 30_000)).toBe(true);
    expect(delays[6]).toBe(30_000);
  });

  it("does not retry after an initial connect failure (graceful skip)", async () => {
    vi.useFakeTimers();
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;

    const delays: number[] = [];
    socket = new DaemonSocket({
      path: t.path,
      autoReconnect: true,
      onScheduleReconnect: (d) => delays.push(d),
    });
    await expect(socket.connect()).rejects.toThrow();

    // Per SPEC § 6.3, a missing daemon is "log, continue without remote" — no
    // retry loop until the session restarts.
    await vi.advanceTimersByTimeAsync(60_000);
    expect(delays).toEqual([]);
  });

  it("reconnects with the same identity and emits 'reconnected' when daemon comes back", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);

    let reconnectCount = 0;
    socket = new DaemonSocket({ path: t.path, autoReconnect: true });
    socket.on("reconnected", () => {
      reconnectCount++;
    });
    await socket.connect();

    // Drop the daemon entirely so the socket sees ECONNRESET.
    for (const c of fake.connections) c.destroy();
    await fake.close();

    // Wait until the client notices the disconnect.
    await vi.waitFor(() => {
      expect(socket?.connected).toBe(false);
    });

    // Bring the daemon back up at the same socket path.
    fake = await startFakeDaemon(t.path);
    await vi.waitFor(
      () => {
        expect(reconnectCount).toBeGreaterThanOrEqual(1);
        expect(socket?.connected).toBe(true);
      },
      { timeout: 8000, interval: 100 },
    );
  });

  it("send() on a closed socket does not throw EPIPE", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);

    socket = new DaemonSocket({ path: t.path, autoReconnect: false });
    await socket.connect();
    socket.close();
    expect(() => socket?.send({ type: "heartbeat", ts: 1 })).not.toThrow();
  });

  it("close() stops further reconnect attempts", async () => {
    vi.useFakeTimers();
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;

    const fakeSocketRef: { current: EventEmitter | undefined } = { current: undefined };
    let attempts = 0;
    const dial = async (): Promise<unknown> => {
      attempts++;
      if (attempts === 1) {
        const s = new EventEmitter() as EventEmitter & {
          write: () => void;
          destroy: () => void;
        };
        s.write = () => {};
        s.destroy = () => {};
        fakeSocketRef.current = s;
        return s;
      }
      throw new Error("ECONNREFUSED");
    };

    const delays: number[] = [];
    socket = new DaemonSocket({
      path: t.path,
      autoReconnect: true,
      onScheduleReconnect: (d) => delays.push(d),
      // biome-ignore lint/suspicious/noExplicitAny: fake socket
      dial: dial as any,
    });
    await socket.connect();
    fakeSocketRef.current?.emit("close");
    expect(delays.length).toBeGreaterThanOrEqual(1);

    socket.close();
    const before = delays.length;
    await vi.advanceTimersByTimeAsync(60_000);
    expect(delays.length).toBe(before);
  });
});
