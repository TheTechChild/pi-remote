// SPDX-License-Identifier: MIT

// Phase-0 skeleton. The factory is wired into Pi via package.json `pi.extensions`.
// Real responsibilities (Unix-socket connect, register, event projection, heartbeat,
// disconnect) land in milestones M1-M7.

interface PiExtensionContext {
  on?: (event: string, handler: (...args: unknown[]) => void) => void;
  log?: (msg: string) => void;
}

export interface PiRemoteExtensionInstance {
  readonly name: "pi-remote-ext";
}

const NAME = "pi-remote-ext" as const;

export default function piRemoteExtensionFactory(
  ctx?: PiExtensionContext,
): PiRemoteExtensionInstance {
  const log = ctx?.log ?? ((msg: string) => console.log(`[${NAME}] ${msg}`));
  ctx?.on?.("session_start", () => log("pi-remote-ext loaded"));
  if (!ctx?.on) {
    log("pi-remote-ext loaded");
  }
  return { name: NAME };
}
