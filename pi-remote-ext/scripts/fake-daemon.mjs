#!/usr/bin/env node
// SPDX-License-Identifier: MIT
//
// Minimal fake daemon for Batch 1 smoke testing. Listens on a Unix socket,
// reads JSONL frames, and replies to every `register` with `accepted: true`.
// Logs every received frame to stdout so you can watch the round-trip.
//
// Usage:
//   node pi-remote-ext/scripts/fake-daemon.mjs        # default /tmp/pi-remote-harness.sock
//   PI_REMOTE_SOCKET=/path/to.sock node pi-remote-ext/scripts/fake-daemon.mjs
import { existsSync, unlinkSync } from "node:fs";
import { createServer } from "node:net";

const socketPath = process.env.PI_REMOTE_SOCKET ?? "/tmp/pi-remote-harness.sock";

if (existsSync(socketPath)) {
  try {
    unlinkSync(socketPath);
  } catch {
    // best effort
  }
}

let connectionSeq = 0;
const server = createServer((conn) => {
  const id = ++connectionSeq;
  console.log(`[fake-daemon] connection #${id} established`);
  let buf = "";
  conn.on("data", (chunk) => {
    buf += chunk.toString("utf8");
    let nl = buf.indexOf("\n");
    while (nl >= 0) {
      const line = buf.slice(0, nl);
      buf = buf.slice(nl + 1);
      if (line.length > 0) handleFrame(conn, id, line);
      nl = buf.indexOf("\n");
    }
  });
  conn.on("close", () => console.log(`[fake-daemon] connection #${id} closed`));
  conn.on("error", (err) => console.log(`[fake-daemon] connection #${id} error: ${err.message}`));
});

function handleFrame(conn, connId, line) {
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    console.log(`[fake-daemon] #${connId} malformed frame: ${line}`);
    return;
  }
  console.log(`[fake-daemon] #${connId} <- ${JSON.stringify(msg)}`);
  if (msg?.type === "register") {
    const ack = {
      type: "register_ack",
      v: 1,
      session_id: msg.session_id,
      accepted: true,
    };
    conn.write(`${JSON.stringify(ack)}\n`);
    console.log(`[fake-daemon] #${connId} -> ${JSON.stringify(ack)}`);
  }
}

server.listen(socketPath, () => {
  console.log(`[fake-daemon] listening on ${socketPath}`);
});

const shutdown = () => {
  console.log("[fake-daemon] shutting down");
  server.close(() => {
    if (existsSync(socketPath)) {
      try {
        unlinkSync(socketPath);
      } catch {
        // best effort
      }
    }
    process.exit(0);
  });
};

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
