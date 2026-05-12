// SPDX-License-Identifier: MIT
import type { Register } from "./proto/extension-daemon/register.js";
import type { RegisterAck } from "./proto/extension-daemon/register_ack.js";

export interface SessionContext {
  sessionId: string;
  cwd: string;
  projectName: string;
  projectDisplayName: string | null;
  tmuxTarget: string;
  pid: number;
  hostname: string;
  model: string;
  startedAt: number;
}

export interface RegisterTarget {
  send(msg: unknown): void;
  on(event: "message", handler: (msg: unknown) => void): unknown;
  off?(event: "message", handler: (msg: unknown) => void): unknown;
  removeListener?(event: "message", handler: (msg: unknown) => void): unknown;
}

const DEFAULT_REGISTER_TIMEOUT_MS = 10_000;

export function buildRegister(ctx: SessionContext): Register {
  const token = process.env.PI_REMOTE_SPAWN_TOKEN;
  return {
    type: "register",
    v: 1,
    session_id: ctx.sessionId,
    spawn_token: token && token.length > 0 ? token : null,
    cwd: ctx.cwd,
    project_name: ctx.projectName,
    project_display_name: ctx.projectDisplayName,
    tmux_target: ctx.tmuxTarget,
    pid: ctx.pid,
    hostname: ctx.hostname,
    model: ctx.model,
    started_at: ctx.startedAt,
  };
}

export interface RegisterOptions {
  timeoutMs?: number;
}

export function registerWithDaemon(
  socket: RegisterTarget,
  payload: Register,
  opts: RegisterOptions = {},
): Promise<RegisterAck> {
  const timeoutMs = opts.timeoutMs ?? DEFAULT_REGISTER_TIMEOUT_MS;
  return new Promise<RegisterAck>((resolve, reject) => {
    let settled = false;
    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      detach();
      reject(new Error(`timed out waiting for register_ack after ${timeoutMs}ms`));
    }, timeoutMs);

    const onMessage = (msg: unknown): void => {
      if (!isRegisterAck(msg)) return;
      if (msg.session_id !== payload.session_id) return;
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      detach();
      if (msg.accepted) {
        resolve(msg);
      } else {
        const reason = msg.reason ?? "no reason given";
        reject(new Error(`register rejected: ${reason}`));
      }
    };

    const detach = (): void => {
      const off = socket.off ?? socket.removeListener;
      off?.call(socket, "message", onMessage);
    };

    socket.on("message", onMessage);
    socket.send(payload);
  });
}

function isRegisterAck(msg: unknown): msg is RegisterAck {
  if (typeof msg !== "object" || msg === null) return false;
  const m = msg as Record<string, unknown>;
  return (
    m.type === "register_ack" && typeof m.session_id === "string" && typeof m.accepted === "boolean"
  );
}
