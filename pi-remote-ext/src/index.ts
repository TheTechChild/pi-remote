// SPDX-License-Identifier: MIT
import { randomUUID } from "node:crypto";
import { hostname } from "node:os";
import { homedir } from "node:os";
import { join } from "node:path";
import { startHeartbeat } from "./heartbeat.js";
import { projectNameFromCwd } from "./project.js";
import type { Disconnect } from "./proto/extension-daemon/disconnect.js";
import { buildRegister, registerWithDaemon } from "./register.js";
import { DaemonSocket, type DaemonSocketOptions } from "./socket.js";

const NAME = "pi-remote-ext" as const;
const TMUX_TARGET_PLACEHOLDER = "untmuxed:0.0";

interface PiExtensionContext {
  on?: (event: string, handler: (...args: unknown[]) => unknown) => void;
  log?: (msg: string) => void;
  cwd?: string;
  pid?: number;
  model?: string;
}

export interface PiRemoteExtensionInstance {
  readonly name: typeof NAME;
  readonly sessionId: string;
  shutdown: () => Promise<void>;
}

export interface PiRemoteFactoryOptions {
  socketPath?: string;
  heartbeatIntervalMs?: number;
  socketFactory?: (opts: DaemonSocketOptions) => DaemonSocket;
}

function defaultSocketPath(): string {
  return process.env.PI_REMOTE_SOCKET && process.env.PI_REMOTE_SOCKET.length > 0
    ? process.env.PI_REMOTE_SOCKET
    : join(homedir(), ".pi-remote", "daemon.sock");
}

export default function piRemoteExtensionFactory(
  ctx?: PiExtensionContext,
  opts: PiRemoteFactoryOptions = {},
): PiRemoteExtensionInstance {
  const log = ctx?.log ?? ((msg: string) => console.log(`[${NAME}] ${msg}`));
  const sessionId = randomUUID();
  const startedAt = Date.now();
  const cwd = ctx?.cwd ?? process.cwd();
  const pid = ctx?.pid ?? process.pid;
  const host = hostname();
  const model = ctx?.model ?? "unknown";
  const socketPath = opts.socketPath ?? defaultSocketPath();
  const makeSocket = opts.socketFactory ?? ((o: DaemonSocketOptions) => new DaemonSocket(o));

  let socket: DaemonSocket | undefined;
  let stopHeartbeat: (() => void) | undefined;
  let shuttingDown = false;
  let disconnectSent = false;

  const sendDisconnect = (reason: Disconnect["reason"]): void => {
    if (disconnectSent || !socket || !socket.connected) return;
    disconnectSent = true;
    const msg: Disconnect = { type: "disconnect", reason };
    socket.send(msg);
  };

  const onSessionStart = async (): Promise<void> => {
    if (socket) return;
    socket = makeSocket({ path: socketPath });

    socket.on("reconnected", () => {
      // After a daemon restart, re-send the register with the same session_id.
      const payload = buildRegister({
        sessionId,
        cwd,
        projectName: projectNameFromCwd(cwd),
        projectDisplayName: null,
        tmuxTarget: TMUX_TARGET_PLACEHOLDER,
        pid,
        hostname: host,
        model,
        startedAt,
      });
      registerWithDaemon(socket as DaemonSocket, payload).catch((err: Error) => {
        log(`re-register after reconnect failed: ${err.message}`);
      });
    });

    try {
      await socket.connect();
    } catch (err) {
      log(`daemon socket unavailable (${(err as Error).message}); continuing without remote`);
      return;
    }

    const payload = buildRegister({
      sessionId,
      cwd,
      projectName: projectNameFromCwd(cwd),
      projectDisplayName: null,
      tmuxTarget: TMUX_TARGET_PLACEHOLDER,
      pid,
      hostname: host,
      model,
      startedAt,
    });

    try {
      await registerWithDaemon(socket, payload);
      log(`registered with daemon (session_id=${sessionId})`);
    } catch (err) {
      log(`register rejected: ${(err as Error).message}`);
      socket.close();
      socket = undefined;
      return;
    }

    stopHeartbeat = startHeartbeat(socket, {
      ...(opts.heartbeatIntervalMs !== undefined ? { intervalMs: opts.heartbeatIntervalMs } : {}),
    });
  };

  const onSessionShutdown = async (): Promise<void> => {
    if (shuttingDown) return;
    shuttingDown = true;
    sendDisconnect("session_shutdown");
    stopHeartbeat?.();
    stopHeartbeat = undefined;
    // Give the disconnect frame a tick to flush before tearing the socket down.
    await new Promise<void>((resolve) => setImmediate(resolve));
    socket?.close();
    socket = undefined;
  };

  ctx?.on?.("session_start", () => {
    void onSessionStart();
  });
  ctx?.on?.("session_shutdown", () => {
    void onSessionShutdown();
  });

  return {
    name: NAME,
    sessionId,
    shutdown: onSessionShutdown,
  };
}
