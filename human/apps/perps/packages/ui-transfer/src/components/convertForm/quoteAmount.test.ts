import { describe, expect, it } from "vitest";
import {
  calculateMinimumReceived,
  calculateQuoteRate,
  getQuoteTargetAmount,
  isCeFiQuote,
  resolveIsCeFiQuote,
} from "./quoteAmount";

describe("convert quote value calculations", () => {
  it("uses estimatedValue directly as the human-readable USDC amount", () => {
    expect(calculateQuoteRate(5, "5.005549")).toBe("1.0011098");
    expect(getQuoteTargetAmount("5.005549", "5005549", 6)).toBe("5.005549");
  });

  it("keeps the output independent of token decimals", () => {
    expect(calculateQuoteRate(5, "5.005549")).not.toBe(
      calculateQuoteRate(5, "5005549"),
    );
  });

  it("calculates minimum received from estimatedValue", () => {
    expect(calculateMinimumReceived("5.005549", "0.5")).toBe("4.980521255");
  });

  it("converts estimatedAmount with token decimals as the quote fallback", () => {
    expect(getQuoteTargetAmount(undefined, "100621", 6)).toBe("0.100621");
  });

  it("rejects non-positive quote fallback amounts", () => {
    expect(getQuoteTargetAmount("0", "0", 6)).toBe("-");
  });

  it("detects CeFi manual swap quotes by their empty chainId", () => {
    expect(isCeFiQuote("")).toBe(true);
    expect(isCeFiQuote("42161")).toBe(false);
    expect(isCeFiQuote(undefined)).toBe(false);
  });

  it("keeps CeFi display rules while the quote reloads or the input is empty", () => {
    // Live matched quote drives the decision when present.
    expect(resolveIsCeFiQuote("", true, undefined)).toBe(true);
    expect(resolveIsCeFiQuote("42161", true, "")).toBe(false);
    // No live quote: fall back to the chainId remembered for the pair.
    expect(resolveIsCeFiQuote(undefined, false, "")).toBe(true);
    expect(resolveIsCeFiQuote(undefined, false, "42161")).toBe(false);
    // Nothing remembered yet: fall back to the token-metadata CeFi hint.
    expect(resolveIsCeFiQuote(undefined, false, undefined, true)).toBe(true);
    expect(resolveIsCeFiQuote(undefined, false, undefined, false)).toBe(false);
    // Remembered routing outranks the metadata hint; live quote outranks all.
    expect(resolveIsCeFiQuote(undefined, false, "", false)).toBe(true);
    expect(resolveIsCeFiQuote(undefined, false, "42161", true)).toBe(false);
    expect(resolveIsCeFiQuote("42161", true, "", true)).toBe(false);
  });
});
