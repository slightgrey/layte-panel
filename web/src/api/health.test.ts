import { describe, expect, it } from "vitest";
import { formatVersion } from "./health";

describe("formatVersion", () => {
  it.each([
    ["0.1.2", "v0.1.2"],
    ["v0.1.1-2-gf570d9b", "v0.1.1-2-gf570d9b"],
    ["dev", "dev"],
  ])("%s → %s", (input, want) => {
    expect(formatVersion(input)).toBe(want);
  });
});
