// SPDX-License-Identifier: MIT
import { basename, dirname } from "node:path";

/*
 * Placeholder for M5 (project name resolution). The real implementation walks
 * up looking for `.pi-remote.toml` and a git root before falling back to the
 * parent directory. For Batch 1 we ship the simple parent-basename rule so
 * `register.project_name` is always a non-empty string.
 */
export function projectNameFromCwd(cwd: string): string {
  return basename(dirname(cwd)) || basename(cwd) || cwd;
}
