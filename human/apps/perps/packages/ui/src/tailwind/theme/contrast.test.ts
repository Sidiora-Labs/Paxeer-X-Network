import { describe, expect, it } from "vitest";
import { getContrastTextRgb } from "./contrast";

describe("getContrastTextRgb", () => {
  it("uses white text on the current deep-purple button", () => {
    expect(getContrastTextRgb("103 0 206")).toBe("255 255 255");
  });

  it("uses black text on a light customer color", () => {
    expect(getContrastTextRgb("255 230 80")).toBe("0 0 0");
  });

  it("rejects malformed RGB theme values", () => {
    expect(() => getContrastTextRgb("103 0")).toThrow(/R G B/);
  });
});
