import Decimal, { getPrecisionByNumber } from "../src/decimal";

describe("decimal", () => {
  test("round down", () => {
    const d = new Decimal(23.32667);
    expect(d.toFixed(2)).toBe("23.32");
  });

  test("get precision by number: Exponential", () => {
    const dp = getPrecisionByNumber(1e-7);

    expect(dp).toBe(7);
  });

  test("get precision by number：0.1", () => {
    const dp = getPrecisionByNumber(0.1);

    expect(dp).toBe(1);
  });
});
