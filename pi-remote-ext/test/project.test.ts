// SPDX-License-Identifier: MIT
import { describe, expect, it } from "vitest";
import { projectNameFromCwd } from "../src/project.js";

describe("projectNameFromCwd (M5 placeholder)", () => {
  it("returns the basename of the parent directory", () => {
    expect(projectNameFromCwd("/Users/clayton/projects/foo")).toBe("projects");
  });

  it("returns the basename of the cwd when cwd is at filesystem root", () => {
    expect(projectNameFromCwd("/")).toBe("/");
  });

  it("handles relative paths by resolving against the parent component only", () => {
    expect(projectNameFromCwd("a/b/c")).toBe("b");
  });
});
