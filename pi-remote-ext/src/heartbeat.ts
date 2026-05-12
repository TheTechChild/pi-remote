// SPDX-License-Identifier: MIT
import type { Heartbeat } from "./proto/extension-daemon/heartbeat.js";

export interface HeartbeatTarget {
  send(msg: unknown): void;
  on(event: "disconnected", handler: () => void): unknown;
  on(event: "reconnected", handler: () => void): unknown;
}

export interface HeartbeatOptions {
  intervalMs?: number;
  now?: () => number;
}

const DEFAULT_INTERVAL_MS = 10_000;

export function startHeartbeat(socket: HeartbeatTarget, opts: HeartbeatOptions = {}): () => void {
  const intervalMs = opts.intervalMs ?? DEFAULT_INTERVAL_MS;
  const now = opts.now ?? Date.now;

  let timer: ReturnType<typeof setInterval> | undefined;
  let stopped = false;

  const armTimer = (): void => {
    if (stopped || timer) return;
    timer = setInterval(() => {
      if (stopped) return;
      const msg: Heartbeat = { type: "heartbeat", ts: now() };
      socket.send(msg);
    }, intervalMs);
  };

  const clearTimer = (): void => {
    if (timer) {
      clearInterval(timer);
      timer = undefined;
    }
  };

  socket.on("disconnected", () => {
    clearTimer();
  });
  socket.on("reconnected", () => {
    if (stopped) return;
    armTimer();
  });

  armTimer();

  return () => {
    if (stopped) return;
    stopped = true;
    clearTimer();
  };
}
