// SPDX-License-Identifier: MIT
import { describe, expect, it } from "vitest";
import { uuidv7 } from "../src/uuidv7.js";

describe("uuidv7", () => {
  it("produces RFC 9562 v7 layout", () => {
    const id = uuidv7();
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  });

  it("encodes the timestamp so ids sort chronologically", () => {
    const earlier = uuidv7(1_700_000_000_000);
    const later = uuidv7(1_700_000_000_001);
    expect(earlier < later).toBe(true);
    // Round-trip the 48-bit millis prefix.
    const millis = Number.parseInt(earlier.slice(0, 8) + earlier.slice(9, 13), 16);
    expect(millis).toBe(1_700_000_000_000);
  });

  it("is unique across rapid generation", () => {
    const seen = new Set(Array.from({ length: 10_000 }, () => uuidv7()));
    expect(seen.size).toBe(10_000);
  });
});
