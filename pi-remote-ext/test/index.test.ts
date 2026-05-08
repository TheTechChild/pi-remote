// SPDX-License-Identifier: MIT
import { describe, expect, it, vi } from "vitest";
import piRemoteExtensionFactory from "../src/index.js";

describe("piRemoteExtensionFactory", () => {
  it("returns an instance named pi-remote-ext", () => {
    const log = vi.fn();
    const instance = piRemoteExtensionFactory({ log });
    expect(instance.name).toBe("pi-remote-ext");
  });

  it("logs on session_start when ctx provides an event bus", () => {
    const log = vi.fn();
    const handlers = new Map<string, (...args: unknown[]) => void>();
    piRemoteExtensionFactory({
      log,
      on: (event, handler) => {
        handlers.set(event, handler);
      },
    });
    handlers.get("session_start")?.();
    expect(log).toHaveBeenCalledWith("pi-remote-ext loaded");
  });

  it("logs immediately when no event bus is provided", () => {
    const log = vi.fn();
    piRemoteExtensionFactory({ log });
    expect(log).toHaveBeenCalledWith("pi-remote-ext loaded");
  });
});
