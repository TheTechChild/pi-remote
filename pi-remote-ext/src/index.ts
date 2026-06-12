// SPDX-License-Identifier: MIT
import { uuidv7 } from "./uuidv7.js";
import { hostname } from "node:os";
import { homedir } from "node:os";
import { join } from "node:path";
import { EVENTS_TABLE, PI_EVENT_NAMES, type PiEventName } from "./events-table.js";
import { startHeartbeat } from "./heartbeat.js";
import { resolveProject } from "./project.js";
import type { Disconnect } from "./proto/extension-daemon/disconnect.js";
import type { Event } from "./proto/extension-daemon/event.js";
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
  const sessionId = uuidv7(); // D17: time-ordered session ids (#46)
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
  // `registered` flips true after the daemon ACKs the register frame and
  // flips back to false on disconnect. The Pi event handlers below drop
  // any projection that fires while it is false, preserving the
  // daemon-side invariant "register is the first frame." See SPEC § 6.4
  // and batch-2 plan, Workstream A "pre-register drop policy."
  let registered = false;

  const projectAndSend = (name: PiEventName, payload: unknown): void => {
    if (!registered) {
      log(`dropped pre-register event: ${name}`);
      return;
    }
    if (!socket || !socket.connected) {
      // No backpressure in Batch 1; if the socket has dropped we are in
      // reconnect territory and the next register_ack will flip
      // `registered` back on.
      log(`dropped event while socket disconnected: ${name}`);
      return;
    }
    let frame: Event | null;
    try {
      frame = EVENTS_TABLE[name](payload);
    } catch (err) {
      log(`projector ${name} threw: ${(err as Error).message}`);
      return;
    }
    if (frame === null) return;
    socket.send(frame);
  };

  const sendDisconnect = (reason: Disconnect["reason"]): void => {
    if (disconnectSent || !socket || !socket.connected) return;
    disconnectSent = true;
    const msg: Disconnect = { type: "disconnect", reason };
    socket.send(msg);
  };

  const onSessionStart = async (): Promise<void> => {
    if (socket) return;
    socket = makeSocket({ path: socketPath });

    socket.on("disconnected", () => {
      // Suspend event projection until the next register_ack flips
      // `registered` back on. Events fired in the disconnect window are
      // dropped per § 7.8 / Workstream A drop policy.
      registered = false;
    });

    socket.on("reconnected", () => {
      // After a daemon restart, re-send the register with the same session_id.
      const resolvedProj = resolveProject(cwd);
      const payload = buildRegister({
        sessionId,
        cwd,
        projectName: resolvedProj.projectName,
        projectDisplayName: resolvedProj.projectDisplayName,
        tmuxTarget: TMUX_TARGET_PLACEHOLDER,
        pid,
        hostname: host,
        model,
        startedAt,
      });
      registerWithDaemon(socket as DaemonSocket, payload)
        .then(() => {
          registered = true;
        })
        .catch((err: Error) => {
          log(`re-register after reconnect failed: ${err.message}`);
        });
    });

    try {
      await socket.connect();
    } catch (err) {
      log(`daemon socket unavailable (${(err as Error).message}); continuing without remote`);
      return;
    }

    const resolvedProj = resolveProject(cwd);
    const payload = buildRegister({
      sessionId,
      cwd,
      projectName: resolvedProj.projectName,
      projectDisplayName: resolvedProj.projectDisplayName,
      tmuxTarget: TMUX_TARGET_PLACEHOLDER,
      pid,
      hostname: host,
      model,
      startedAt,
    });

    try {
      await registerWithDaemon(socket, payload);
      log(`registered with daemon (session_id=${sessionId})`);
      registered = true;
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
    registered = false;
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

  // Register one synchronous handler per projectable Pi event. The handler
  // is intentionally tiny and synchronous — projection order matches Pi's
  // event-emit order, with no setImmediate / setTimeout(0) reordering.
  for (const name of PI_EVENT_NAMES) {
    ctx?.on?.(name, (payload: unknown) => {
      projectAndSend(name, payload);
    });
  }

  return {
    name: NAME,
    sessionId,
    shutdown: onSessionShutdown,
  };
}
