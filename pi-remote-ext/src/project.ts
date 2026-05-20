// SPDX-License-Identifier: MIT
import { existsSync, readFileSync } from "node:fs";
import { basename, dirname, resolve } from "node:path";
import { parse } from "smol-toml";

export interface ResolvedProject {
  projectName: string;
  projectDisplayName: string | null;
}

/**
 * Resolves the project name and optional display name for a given cwd (M5).
 * 1. Walks up looking for a `.pi-remote.toml`. If found and contains `project = "..."`, uses that.
 * 2. Walks up looking for a git root (containing `.git/`). If found, uses the basename of the git root.
 * 3. Falls back to the basename of the immediate parent directory of cwd.
 */
export function resolveProject(cwd: string): ResolvedProject {
  let dir = resolve(cwd);
  while (true) {
    const tomlPath = resolve(dir, ".pi-remote.toml");
    if (existsSync(tomlPath)) {
      try {
        const content = readFileSync(tomlPath, "utf-8");
        const parsed = parse(content) as Record<string, unknown>;
        if (typeof parsed.project === "string" && parsed.project.trim().length > 0) {
          return {
            projectName: parsed.project,
            projectDisplayName:
              typeof parsed.display_name === "string" ? parsed.display_name : null,
          };
        }
      } catch (e) {
        // Ignore parse error and continue walking
      }
    }

    const gitPath = resolve(dir, ".git");
    if (existsSync(gitPath)) {
      return {
        projectName: basename(dir) || dir,
        projectDisplayName: null,
      };
    }

    const parent = dirname(dir);
    if (parent === dir) {
      break;
    }
    dir = parent;
  }

  // Fallback to parent directory of the initial cwd
  const parentDir = dirname(resolve(cwd));
  const fallbackName = basename(parentDir) || basename(resolve(cwd)) || resolve(cwd);
  return {
    projectName: fallbackName,
    projectDisplayName: null,
  };
}

/**
 * Kept for backward compatibility, returns resolveProject(cwd).projectName.
 */
export function projectNameFromCwd(cwd: string): string {
  return resolveProject(cwd).projectName;
}
