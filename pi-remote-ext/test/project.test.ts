// SPDX-License-Identifier: MIT
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { projectNameFromCwd, resolveProject } from "../src/project.js";

describe("project resolution (M5)", () => {
  let tempDir: string;

  beforeEach(async () => {
    tempDir = await mkdtemp(join(tmpdir(), "pi-remote-project-test-"));
  });

  afterEach(async () => {
    await rm(tempDir, { recursive: true, force: true });
  });

  it("reads .pi-remote.toml walk-up override", async () => {
    const rootDir = join(tempDir, "workspace");
    const subDir = join(rootDir, "packages", "api");
    await mkdir(subDir, { recursive: true });

    // Write .pi-remote.toml in rootDir
    await writeFile(
      join(rootDir, ".pi-remote.toml"),
      `
      project = "my-custom-project"
      display_name = "My Custom Project"
      `,
    );

    const result = resolveProject(subDir);
    expect(result.projectName).toBe("my-custom-project");
    expect(result.projectDisplayName).toBe("My Custom Project");
  });

  it("reads git-root basename walk-up if no .pi-remote.toml override exists", async () => {
    const rootDir = join(tempDir, "my-git-repo");
    const subDir = join(rootDir, "src", "components");
    await mkdir(join(rootDir, ".git"), { recursive: true });
    await mkdir(subDir, { recursive: true });

    const result = resolveProject(subDir);
    expect(result.projectName).toBe("my-git-repo");
    expect(result.projectDisplayName).toBeNull();
  });

  it("falls back to parent directory of cwd when neither exists", async () => {
    const rootDir = join(tempDir, "parent-folder");
    const subDir = join(rootDir, "child-folder");
    await mkdir(subDir, { recursive: true });

    const result = resolveProject(subDir);
    expect(result.projectName).toBe("parent-folder");
    expect(result.projectDisplayName).toBeNull();
  });

  it("handles filesystem root gracefully", () => {
    const result = resolveProject("/");
    expect(result.projectName).toBe("/");
    expect(result.projectDisplayName).toBeNull();
  });

  it("projectNameFromCwd helper works backward-compatibly", async () => {
    const rootDir = join(tempDir, "parent-folder");
    const subDir = join(rootDir, "child-folder");
    await mkdir(subDir, { recursive: true });

    expect(projectNameFromCwd(subDir)).toBe("parent-folder");
  });
});
