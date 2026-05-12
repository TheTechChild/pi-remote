// SPDX-License-Identifier: MIT
import { mkdtemp, rm } from "node:fs/promises";
import { type Server, type Socket, createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import piRemoteExtensionFactory from "../src/index.js";

interface FakeDaemon {
  server: Server;
  path: string;
  connections: Socket[];
  // Each connection's parsed JSONL frames.
  frames: unknown[][];
  ackOn?: (sessionId: string) => { accepted: boolean; reason?: string };
  close: () => Promise<void>;
}

async function startFakeDaemon(socketPath: string): Promise<FakeDaemon> {
  const fake: FakeDaemon = {
    // biome-ignore lint/style/noNonNullAssertion: assigned below
    server: undefined!,
    path: socketPath,
    connections: [],
    frames: [],
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

  function maybeAck(conn: Socket, parsed: unknown): void {
    if (typeof parsed !== "object" || parsed === null) return;
    const msg = parsed as Record<string, unknown>;
    if (msg.type !== "register") return;
    const decision = fake.ackOn?.(String(msg.session_id)) ?? { accepted: true };
    const ack: Record<string, unknown> = {
      type: "register_ack",
      v: 1,
      session_id: msg.session_id,
      accepted: decision.accepted,
    };
    if (decision.reason) ack.reason = decision.reason;
    conn.write(`${JSON.stringify(ack)}\n`);
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
  const dir = await mkdtemp(join(tmpdir(), "pi-remote-ext-itest-"));
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
}

describe("piRemoteExtensionFactory", () => {
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
    // biome-ignore lint/performance/noDelete: process.env coerces undefined to the string "undefined"; delete is the only way to clear.
    delete process.env.PI_REMOTE_SOCKET;
    // biome-ignore lint/performance/noDelete: see above.
    delete process.env.PI_REMOTE_SPAWN_TOKEN;
  });

  it("returns an instance named pi-remote-ext", () => {
    const ctx = new FakeCtx();
    const inst = piRemoteExtensionFactory(ctx);
    expect(inst.name).toBe("pi-remote-ext");
  });

  it("connects, registers, and heartbeats on session_start", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);
    process.env.PI_REMOTE_SOCKET = t.path;

    const ctx = new FakeCtx();
    const inst = piRemoteExtensionFactory(ctx, { heartbeatIntervalMs: 50 });
    await ctx.fire("session_start");

    await vi.waitFor(() => {
      expect(fake?.frames[0]?.length).toBeGreaterThanOrEqual(1);
    });
    const register = fake?.frames[0]?.[0] as Record<string, unknown>;
    expect(register.type).toBe("register");
    expect(register.v).toBe(1);
    expect(typeof register.session_id).toBe("string");
    expect(register.spawn_token).toBeNull();
    expect(register.tmux_target).toBe("untmuxed:0.0");

    // At least one heartbeat should arrive.
    await vi.waitFor(
      () => {
        const heartbeats = (fake?.frames[0] ?? []).filter(
          (f) => (f as Record<string, unknown>).type === "heartbeat",
        );
        expect(heartbeats.length).toBeGreaterThanOrEqual(1);
      },
      { timeout: 2000 },
    );

    await inst.shutdown?.();
  });

  it("sends a disconnect frame on session_shutdown", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);
    process.env.PI_REMOTE_SOCKET = t.path;

    const ctx = new FakeCtx();
    const inst = piRemoteExtensionFactory(ctx, { heartbeatIntervalMs: 50 });
    await ctx.fire("session_start");
    await vi.waitFor(() => {
      expect(fake?.frames[0]?.length).toBeGreaterThanOrEqual(1);
    });

    await ctx.fire("session_shutdown");

    await vi.waitFor(() => {
      const types = (fake?.frames[0] ?? []).map((f) => (f as Record<string, unknown>).type);
      expect(types).toContain("disconnect");
    });
    const disc = (fake?.frames[0] ?? []).find(
      (f) => (f as Record<string, unknown>).type === "disconnect",
    ) as Record<string, unknown>;
    expect(disc.reason).toBe("session_shutdown");

    await inst.shutdown?.();
  });

  it("re-registers with the same session_id after a daemon restart", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    fake = await startFakeDaemon(t.path);
    process.env.PI_REMOTE_SOCKET = t.path;

    const ctx = new FakeCtx();
    const inst = piRemoteExtensionFactory(ctx, { heartbeatIntervalMs: 50 });
    await ctx.fire("session_start");

    await vi.waitFor(() => {
      expect(fake?.frames[0]?.length).toBeGreaterThanOrEqual(1);
    });
    const firstSessionId = (fake?.frames[0]?.[0] as Record<string, unknown>).session_id as string;

    // Drop and restart the fake daemon.
    for (const c of fake.connections) c.destroy();
    await fake.close();
    fake = await startFakeDaemon(t.path);

    await vi.waitFor(
      () => {
        expect(fake?.frames[0]?.length).toBeGreaterThanOrEqual(1);
        const reReg = fake?.frames[0]?.[0] as Record<string, unknown>;
        expect(reReg.type).toBe("register");
        expect(reReg.session_id).toBe(firstSessionId);
      },
      { timeout: 8000, interval: 100 },
    );

    await inst.shutdown?.();
  });

  it("does not throw when no daemon is listening on session_start", async () => {
    const t = await makeTempSocketPath();
    cleanup = t.cleanup;
    process.env.PI_REMOTE_SOCKET = t.path;

    const ctx = new FakeCtx();
    const inst = piRemoteExtensionFactory(ctx);
    await expect(Promise.resolve(ctx.fire("session_start"))).resolves.not.toThrow();
    await inst.shutdown?.();
  });
});
