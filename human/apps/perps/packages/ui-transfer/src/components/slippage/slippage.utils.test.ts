import { describe, expect, it } from "vitest";
import {
  getSlippageRiskLevel,
  shouldDisableSlippageSave,
} from "./slippage.utils";

describe("slippage risk guidance", () => {
  it.each([
    [0.1, "minimum"],
    [0.2, "low"],
    [0.49, "low"],
    [0.5, undefined],
    [0.51, "high"],
    [10, "high"],
  ] as const)("maps %s to %s", (value, expected) => {
    expect(getSlippageRiskLevel(value, 0.2, 0.5)).toBe(expected);
  });

  it.each([
    [undefined, undefined, true],
    [0, undefined, true],
    [5, undefined, false],
    [0.1, "minimum", true],
    [0.3, "low", false],
    [1, "high", false],
  ] as const)(
    "sets save disabled for value %s and risk level %s to %s",
    (value, riskLevel, expected) => {
      expect(shouldDisableSlippageSave(value, riskLevel)).toBe(expected);
    },
  );
});
