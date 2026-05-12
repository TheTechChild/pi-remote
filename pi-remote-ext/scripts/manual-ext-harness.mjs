#!/usr/bin/env node
// SPDX-License-Identifier: MIT
//
// Drives pi-remote-ext from a fake Pi-like context for local smoke testing
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
//
// Interactive sub-prompt (M3 / Workstream A): once the harness has connected
// and registered, it reads lines on stdin and fires the corresponding Pi
// event into the registered handler. Recognized commands:
//
//   agent_start
//   agent_end                   # empty messages
//   attention_dialog            # extension_ui_request fixture
//   tool_failure                # tool_execution_end with isError:true
//   tool_ok                     # tool_execution_end with isError:false (no emit)
//   queue_update <N>            # default 1
//   model_select <id>           # default anthropic/harness
//   compaction_start
//   compaction_end
//   extension_error <msg...>    # default "harness error"
//   quit | exit                 # graceful shutdown
//
// Ctrl-C / Ctrl-D also shut down cleanly.

import { createInterface } from "node:readline";
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

function fire(name, payload) {
  const h = handlers.get(name);
  if (!h) {
    console.log(`[harness] no handler registered for "${name}"`);
    return;
  }
  h(payload);
}

function dispatch(line) {
  const trimmed = line.trim();
  if (trimmed.length === 0) return;
  const [cmd, ...rest] = trimmed.split(/\s+/);
  switch (cmd) {
    case "agent_start":
      fire("agent_start", { type: "agent_start" });
      break;
    case "agent_end":
      fire("agent_end", { type: "agent_end", messages: [] });
      break;
    case "attention_dialog":
      fire("extension_ui_request", {
        type: "extension_ui_request",
        method: "prompt",
        title: "Continue?",
        options: ["yes", "no"],
      });
      break;
    case "tool_failure":
      fire("tool_execution_end", {
        type: "tool_execution_end",
        toolName: rest[0] ?? "bash",
        isError: true,
        result: rest.slice(1).join(" ") || "harness-induced failure",
      });
      break;
    case "tool_ok":
      fire("tool_execution_end", {
        type: "tool_execution_end",
        toolName: rest[0] ?? "read",
        isError: false,
        result: "ok",
      });
      break;
    case "queue_update": {
      const pending = Number.parseInt(rest[0] ?? "1", 10);
      fire("queue_update", {
        type: "queue_update",
        pending: Number.isFinite(pending) ? pending : 1,
      });
      break;
    }
    case "model_select":
      fire("model_select", {
        type: "model_select",
        model: { id: rest[0] ?? "anthropic/harness" },
      });
      break;
    case "compaction_start":
      fire("compaction_start", { type: "compaction_start" });
      break;
    case "compaction_end":
      fire("compaction_end", { type: "compaction_end" });
      break;
    case "extension_error":
      fire("extension_error", {
        type: "extension_error",
        error: rest.join(" ") || "harness error",
      });
      break;
    case "quit":
    case "exit":
      void shutdown();
      break;
    case "help":
    case "?":
      console.log(
        "commands: agent_start | agent_end | attention_dialog | tool_failure [name [msg...]] | " +
          "tool_ok | queue_update [N] | model_select [id] | compaction_start | compaction_end | " +
          "extension_error [msg...] | quit",
      );
      break;
    default:
      console.log(`[harness] unknown command: ${cmd} (try "help")`);
  }
}

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

const rl = createInterface({ input: process.stdin, output: process.stdout, terminal: false });
rl.on("line", dispatch);
rl.on("close", shutdown);
