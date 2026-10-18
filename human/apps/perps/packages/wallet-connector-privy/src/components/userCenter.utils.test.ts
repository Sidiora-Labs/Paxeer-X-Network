import { describe, expect, it } from "vitest";
import { shouldUseBlackIcon } from "./userCenter.utils";

describe("shouldUseBlackIcon", () => {
  it("uses black for an explicit black contrast token", () => {
    expect(shouldUseBlackIcon("0 0 0", "dark")).toBe(true);
  });

  it("uses white for an explicit white contrast token", () => {
    expect(shouldUseBlackIcon("255 255 255", "light")).toBe(false);
  });

  it("does not throw for CSS variable references", () => {
    expect(() =>
      shouldUseBlackIcon("var(--brand-contrast)", "light"),
    ).not.toThrow();
    expect(shouldUseBlackIcon("var(--brand-contrast)", "light")).toBe(true);
  });

  it("falls back to the theme mode for missing values", () => {
    expect(shouldUseBlackIcon(undefined, "light")).toBe(true);
    expect(shouldUseBlackIcon(undefined, "dark")).toBe(false);
  });
});
