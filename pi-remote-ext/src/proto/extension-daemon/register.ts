// SPDX-License-Identifier: MIT
// GENERATED — DO NOT EDIT. Source: pi-remote-spec/protocol/extension-daemon/register.json

/**
 * Extension -> Daemon. First message after the extension connects to the Unix socket. See SPEC.md § 10.1.
 */
export interface Register {
  type: "register";
  v: 1;
  /**
   * Pi session UUID (UUIDv7).
   */
  session_id: string;
  /**
   * Hex-encoded 32-character spawn token (D6) when the daemon spawned this Pi process; null/absent when user-spawned.
   */
  spawn_token?: string | null;
  cwd: string;
  project_name: string;
  project_display_name?: string | null;
  /**
   * tmux pane target string in `session:window.pane` form.
   */
  tmux_target: string;
  pid: number;
  hostname: string;
  model: string;
  /**
   * Unix epoch milliseconds at which the Pi process started.
   */
  started_at: number;
}
