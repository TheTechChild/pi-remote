#!/usr/bin/env node
// SPDX-License-Identifier: MIT
//
// Drives pi-remote-ext from a fake Pi-like context for the Batch 1 smoke test
// before tmux/coordinator wiring exists. Run `yarn build` first so the JS
// artifact under dist/ is present. Pair with `scripts/fake-daemon.mjs` (or
// the real daemon once it lands) on the configured PI_REMOTE_SOCKET path.
//
// Usage:
//   yarn build
//   node pi-remote-ext/scripts/fake-daemon.mjs                 # terminal A
//   node pi-remote-ext/scripts/manual-ext-harness.mjs          # terminal B
//
// Both scripts honor PI_REMOTE_SOCKET; when unset they default to
// /tmp/pi-remote-harness.sock.
import factory from "../dist/index.js";

const handlers = new Map();

const ctx = {
  log: (m) => console.log(`[harness] ${m}`),
  on: (ev, h) => handlers.set(ev, h),
  cwd: process.cwd(),
  pid: process.pid,
  model: process.env.PI_REMOTE_HARNESS_MODEL ?? "harness/local",
};

if (!process.env.PI_REMOTE_SOCKET) {
  process.env.PI_REMOTE_SOCKET = "/tmp/pi-remote-harness.sock";
}

const inst = factory(ctx);
console.log(`[harness] session_id=${inst.sessionId} socket=${process.env.PI_REMOTE_SOCKET}`);

handlers.get("session_start")?.();

let shuttingDown = false;
const shutdown = async () => {
  if (shuttingDown) return;
  shuttingDown = true;
  console.log("[harness] shutting down");
  handlers.get("session_shutdown")?.();
  await inst.shutdown();
  setTimeout(() => process.exit(0), 100);
};

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
